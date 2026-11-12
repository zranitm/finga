// @vitest-environment node
// Live server only: FINGA_LIVE_URL=http://localhost:8080 FINGA_LIVE_DELAY_MS=100 npx vitest run prediction.live
import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it, vi } from 'vitest';

import { tickTiming } from '@/lib/fixed-step';
import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import { params } from '@/sim/step';

import { InputSender } from './input-sender';
import { Predictor, wireState } from './prediction';
import type { Reconciled } from './prediction';

const url = process.env.FINGA_LIVE_URL;
const delayMs = Number(process.env.FINGA_LIVE_DELAY_MS ?? '0');
const TICK_MS = 50;
const TIMING = tickTiming(20, params.dt);
const STEPS = TIMING.stepsPerTick; // three 1/60 s steps a tick

type Wire = Data<'snapshot'>['players'][number];
type Keys = Partial<Record<'left' | 'right' | 'up' | 'down' | 'jump', boolean>>;

// Short enough to stay clear of the town walls, where a stopped player would hide a disagreement.
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
  readonly drift: number;
}

/** `gapAt` stalls the client for `gapTicks` ticks: nothing sent or predicted. */
async function play(ticks: number, gapAt = -1, gapTicks = 0): Promise<Run> {
  const origin = url!;
  const town = parseMap(
    'town',
    JSON.parse(readFileSync(path.resolve(process.cwd(), 'public/maps/town.tmj'), 'utf8')),
  );
  const stamp = (Date.now() % 1e8).toString(36);
  const nodeFetch = globalThis.fetch;
  vi.stubGlobal('fetch', (p: string, init: RequestInit) => nodeFetch(origin + p, init)); // node has no page origin
  const rest = createRestClient();
  await rest.register(`live${stamp}`, 'correct horse battery');
  await rest.login(`live${stamp}`, 'correct horse battery');
  const c = await rest.createCharacter(`P${stamp}`);

  const socket = new GameSocket({ url: origin.replace(/^http/, 'ws') + '/ws', delayMs });
  const predictor = new Predictor(town.geometry, TIMING, params);
  const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
  const results: Reconciled[] = [];
  let placed = false;
  let lastServer: Wire | undefined;
  const me = (players: readonly Wire[]) => players.find((p) => p.id === c.id);
  socket.on('snapshot', (m) => {
    const w = me(m.d.players);
    if (w && !placed) {
      predictor.reset(wireState(w));
      placed = true;
    }
  });
  socket.on('delta', (m) => {
    const w = me(m.d.players);
    if (w && placed) {
      lastServer = w;
      const before = predictor.history.find((e) => e.seq, m.d.ackSeq);
      const r = predictor.reconcile(wireState(w), m.d.ackSeq);
      if (r === 'reset' && before) {
        console.log(
          `reset at ack ${m.d.ackSeq}: dx ${(before.state.x - w.x).toFixed(2)} dy ${(before.state.y - w.y).toFixed(2)}`,
        );
      }
      if (process.env.FINGA_LIVE_TRACE) {
        console.log(
          `delta ack ${m.d.ackSeq} sent ${socket.sequence} -> ${r} server x ${w.x.toFixed(1)} pred x ${predictor.state.x.toFixed(1)}`,
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
      continue;
    }
    // End standing still so the server's five-tick hold replays a stop.
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
    for (let i = 0; i < STEPS; i++) {
      predictor.subStep();
    }
    // Absolute timeline; a pause longer than a tick is dropped, not sent as a burst.
    const due = started + (tick + 1) * TICK_MS;
    const now = Date.now();
    if (now > due + TICK_MS) {
      started += now - due;
    } else {
      await sleep(due - now);
    }
  }
  await sleep(TICK_MS * 6 + delayMs * 2);
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
