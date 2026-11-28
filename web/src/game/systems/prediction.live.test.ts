// @vitest-environment node
import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { tickTiming } from '@/lib/fixed-step';
import { createRestClient } from '@/net/rest';
import { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import type { Box, Cylinder, Geometry } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { InputSender } from './input-sender';
import { Predictor, movementParams, wireState } from './prediction';

const url = process.env.FINGA_LIVE_URL;
const delayMs = Number(process.env.FINGA_LIVE_DELAY_MS ?? '0');
const TICK_MS = 50;
const TIMING = tickTiming(20, movement.dt);
const STEPS = TIMING.stepsPerTick; // three 1/60 s steps a tick

type Wire = Data<'snapshot'>['players'][number];
type Keys = Partial<Record<'left' | 'right' | 'up' | 'down' | 'jump', boolean>>;

/** The town as the Go build wrote it for the client (npm run maps). */
function town(): Geometry {
  const file = path.resolve(process.cwd(), 'public/maps/town.json');
  const m = JSON.parse(readFileSync(file, 'utf8')) as {
    geometry: {
      size_x: number;
      size_z: number;
      terrain: { cell: number; nx: number; nz: number; heights: number[] };
      boxes: Box[] | null;
      cylinders: Cylinder[] | null;
    };
  };
  const g = m.geometry;
  const t = g.terrain;
  return {
    size_x: g.size_x,
    size_z: g.size_z,
    terrain: terrain(t.cell, t.nx, t.nz, t.heights, movement.max_slope),
    boxes: g.boxes ?? [],
    cylinders: g.cylinders ?? [],
  };
}

// From the spawn at (14, 16): east, south, west and north round a loop that drifts east, clear of the shop, the well and the plaza's ramp.
function script(tick: number): Keys {
  const phase = tick % 36;
  if (phase === 6) return { right: true, jump: true };
  if (phase < 12) return { right: true };
  if (phase < 18) return { down: true };
  if (phase === 18) return { left: true, jump: true };
  if (phase < 24) return { left: true };
  if (phase < 30) return { up: true };
  return {};
}

interface Run {
  readonly resetsAfter: number[]; // the resets so far after each Delta reconciled
  readonly resets: number;
  readonly drift: number;
}

/** `gapAt` stalls the client for `gapTicks` ticks: nothing sent or predicted. */
async function play(ticks: number, gapAt = -1, gapTicks = 0): Promise<Run> {
  const origin = url!;
  const stamp = (Date.now() % 1e8).toString(36);
  const nodeFetch = globalThis.fetch;
  vi.stubGlobal('fetch', (p: string, init: RequestInit) => nodeFetch(origin + p, init)); // node has no page origin
  const rest = createRestClient();
  await rest.register(`live${stamp}`, 'correct horse battery');
  await rest.login(`live${stamp}`, 'correct horse battery');
  const c = await rest.createCharacter(`P${stamp}`);

  const socket = new GameSocket({ url: origin.replace(/^http/, 'ws') + '/ws', delayMs });
  const predictor = new Predictor(town(), TIMING, movementParams(0), () => []);
  const sender = new InputSender({
    send: (input, tick, view) => socket.sendInput(input, tick, view),
    walk: () => false,
  });
  const resetsAfter: number[] = [];
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
      predictor.reconcile(wireState(w), m.d.ackSeq, m.tick);
      if (process.env.FINGA_LIVE_TRACE) {
        console.log(
          `delta ack ${m.d.ackSeq} sent ${socket.sequence} resets ${predictor.resets} server (${w.x.toFixed(2)}, ${w.z.toFixed(2)}) pred (${predictor.state.x.toFixed(2)}, ${predictor.state.z.toFixed(2)})`,
        );
      }
      resetsAfter.push(predictor.resets);
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
    // End standing for a second, past a jump's 0.65 s in the air, so the last Delta is the player at rest.
    const keys = tick >= ticks - 20 ? {} : script(tick);
    sender.releaseAll();
    for (const k of ['left', 'right', 'up', 'down', 'jump'] as const) {
      if (keys[k]) {
        sender.press(k);
      }
    }
    const input = sender.tickStart(0);
    predictor.beginTick(socket.sequence, input, 0);
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

  const resets = predictor.resets;
  const s = predictor.state;
  const drift = lastServer
    ? Math.hypot(s.x - lastServer.x, s.y - lastServer.y, s.z - lastServer.z)
    : Number.NaN;
  console.log(
    `live prediction: ${resetsAfter.length} deltas, ${resets} resets, drift ${drift.toFixed(3)} m, delay ${delayMs} ms`,
  );
  return { resetsAfter, resets, drift };
}

// Against a live server only: FINGA_LIVE_URL=http://localhost:8080 FINGA_LIVE_DELAY_MS=100 npx vitest run prediction.live
describe.skipIf(!url)('prediction against the live server', () => {
  it('a hundred ticks of scripted movement never reset under jitter alone', async () => {
    const run = await play(100);
    expect(run.resets).toBe(0);
    expect(run.resetsAfter.length).toBeGreaterThan(50);
    expect(run.drift).toBe(0);
  }, 30_000);

  it('a three-tick stall is held by the server and still matches', async () => {
    const run = await play(60, 15, 3);
    expect(run.resets).toBe(0);
    expect(run.drift).toBe(0);
  }, 30_000);

  it('a stall longer than the hold is the one real disagreement: a single reset, then exact again', async () => {
    const run = await play(60, 15, 8);
    expect(run.resets).toBe(1); // and exact again after it
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
