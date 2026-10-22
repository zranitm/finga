// @vitest-environment node
// A live check of prediction against a running server (make up), run only
// when FINGA_LIVE_URL is set:
//
//   FINGA_LIVE_URL=http://localhost:8080 FINGA_LIVE_DELAY_MS=100 npx vitest run prediction.live
//
// It plays scripted seconds of walking, jumping and stopping through the
// real GameSocket, one Input per tick, and expects every Delta to match the
// prediction (no reset) under timing jitter alone, with and without the
// development network delay. A stall longer than the server's hold (five
// ticks, backend step 18) is the design's real disagreement: one reset.
import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';

import { InputSender } from './input-sender';
import { Predictor, STEPS_PER_TICK } from './prediction';
import type { Reconciled } from './prediction';

const url = process.env.FINGA_LIVE_URL;
const delayMs = Number(process.env.FINGA_LIVE_DELAY_MS ?? '0');
const TICK_MS = 50;

type Wire = Data<'Snapshot'>['players'][number];
type Keys = Partial<Record<'left' | 'right' | 'up' | 'down' | 'jump', boolean>>;

/**
 * The keys held on a tick: twelve ticks right with a jump, twelve back with a
 * jump, twelve standing; short enough to stay clear of the town walls, where a
 * stopped player would hide a disagreement.
 */
function script(tick: number): Keys {
  const phase = tick % 36;
  if (phase === 6) return { right: true, jump: true };
  if (phase < 12) return { right: true };
  if (phase === 18) return { left: true, jump: true };
  if (phase < 24) return { left: true };
  return {};
}

interface Run {
  readonly results: Reconciled[];
  readonly resets: number;
  readonly matches: number;
  readonly drift: number; // px between the prediction and the last server state
}

/**
 * Plays the script for the given ticks through a fresh character. `gapAt`
 * skips sending (and predicting) for `gapTicks` ticks at that tick, as a
 * stalled client would.
 */
async function play(ticks: number, gapAt = -1, gapTicks = 0): Promise<Run> {
  const origin = url!;
  const town = parseMap(
    'town',
    JSON.parse(readFileSync(path.resolve(process.cwd(), 'public/maps/town.tmj'), 'utf8')),
  );
  const stamp = (Date.now() % 1e8).toString(36);
  const rest = createRestClient({ origin });
  await rest.register(`live${stamp}`, 'correct horse battery');
  await rest.login(`live${stamp}`, 'correct horse battery');
  const c = await rest.createCharacter(`P${stamp}`, 'warrior');

  const socket = new GameSocket({ url: origin.replace(/^http/, 'ws') + '/ws', delayMs });
  const predictor = new Predictor(town.geometry);
  const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
  const results: Reconciled[] = [];
  let placed = false;
  let lastServer: Wire | undefined;
  const me = (players: readonly Wire[]) => players.find((p) => p.id === c.id);
  socket.on('Snapshot', (m) => {
    const w = me(m.d.players);
    if (w && !placed) {
      predictor.reset(w);
      placed = true;
    }
  });
  socket.on('Delta', (m) => {
    const w = me(m.d.players);
    if (w && placed) {
      lastServer = w;
      const before = predictor.history.find((e) => e.seq, m.d.ack_seq);
      const r = predictor.reconcile(w, m.d.ack_seq);
      if (r === 'reset' && before) {
        console.log(
          `reset at ack ${m.d.ack_seq}: dx ${(before.state.x - w.x).toFixed(2)} dy ${(before.state.y - w.y).toFixed(2)}`,
        );
      }
      if (process.env.FINGA_LIVE_TRACE) {
        console.log(
          `delta ack ${m.d.ack_seq} sent ${socket.sequence} -> ${r} server x ${w.x.toFixed(1)} pred x ${predictor.state.x.toFixed(1)}`,
        );
      }
      results.push(r);
    }
  });
  await socket.connect(c.id);
  await waitFor(() => placed, 3000);

  let started = Date.now();
  for (let tick = 0; tick < ticks; tick++) {
    if (tick >= gapAt && tick < gapAt + gapTicks) {
      await sleep(Math.max(0, started + (tick + 1) * TICK_MS - Date.now()));
      continue; // stalled on purpose: nothing sent, nothing predicted
    }
    // The last ticks stand still, so the server holds nothing but a stop after
    // the run ends (its five-tick hold then falls back to the held keys).
    const keys = tick >= ticks - 6 ? {} : script(tick);
    sender.releaseAll();
    for (const k of ['left', 'right', 'up', 'down', 'jump'] as const) {
      if (keys[k]) {
        sender.press(k);
      }
    }
    const input = sender.tickStart();
    predictor.beginTick(socket.sequence, input);
    if (process.env.FINGA_LIVE_TRACE) {
      console.log(
        `tick ${tick} seq ${socket.sequence} at ${Date.now() - started} ms x ${predictor.state.x.toFixed(1)} ${JSON.stringify(input)}`,
      );
    }
    for (let i = 0; i < STEPS_PER_TICK; i++) {
      predictor.subStep();
    }
    // An absolute timeline: the loop does not drift by its own overhead. After a
    // pause of the process longer than a tick, the missed time is dropped rather
    // than sent as a burst, as lib/fixed-step does in the game.
    const due = started + (tick + 1) * TICK_MS;
    const now = Date.now();
    if (now > due + TICK_MS) {
      started += now - due;
    } else {
      await sleep(due - now);
    }
  }
  await sleep(TICK_MS * 6 + delayMs * 2); // the last Deltas arrive
  socket.close();

  const resets = results.filter((r) => r === 'reset').length;
  const matches = results.filter((r) => r === 'match').length;
  const drift = lastServer
    ? Math.hypot(predictor.state.x - lastServer.x, predictor.state.y - lastServer.y)
    : Number.NaN;
  console.log(
    `live prediction: ${results.length} deltas, ${matches} matches, ${resets} resets, drift ${drift.toFixed(2)} px, delay ${delayMs} ms`,
  );
  return { results, resets, matches, drift };
}

describe.skipIf(!url)('prediction against the live server', () => {
  it('a hundred ticks of scripted movement never reset under jitter alone', async () => {
    const run = await play(100);
    expect(run.resets).toBe(0);
    expect(run.matches).toBeGreaterThan(50);
    expect(run.drift).toBe(0);
  }, 30_000);

  it('a three-tick stall is held by the server and still matches', async () => {
    const run = await play(60, 15, 3);
    expect(run.resets).toBe(0);
    expect(run.drift).toBe(0);
  }, 30_000);

  it('a stall longer than the hold is the one real disagreement: a single reset, then exact again', async () => {
    const run = await play(60, 15, 8);
    expect(run.resets).toBe(1);
    expect(run.results.slice(run.results.lastIndexOf('reset') + 1)).not.toContain('reset');
    expect(run.drift).toBe(0);
  }, 30_000);
});

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

async function waitFor(cond: () => boolean, timeoutMs: number): Promise<void> {
  const until = Date.now() + timeoutMs;
  while (!cond()) {
    if (Date.now() > until) {
      throw new Error('timed out');
    }
    await sleep(10);
  }
}
