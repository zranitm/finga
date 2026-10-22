import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { noInput } from '@/sim/input';
import type { Input } from '@/sim/input';
import { parseMap } from '@/sim/map';
import { NO_PLATFORM, params, standing, step } from '@/sim/step';
import type { State } from '@/sim/step';

import { HISTORY_TICKS, Predictor, STEPS_PER_TICK, fromWire, platformUnder } from './prediction';
import type { WireState } from './prediction';

const town = parseMap(
  'town',
  JSON.parse(readFileSync(path.resolve(process.cwd(), 'public/maps/town.tmj'), 'utf8')),
);
const g = town.geometry;
const spawn = town.player_spawn;

const R: Input = { ...noInput, right: true };
const L: Input = { ...noInput, left: true };
const RJ: Input = { ...noInput, right: true, jump: true };
const DJ: Input = { ...noInput, down: true, jump: true };

/** sim/step run directly, three sub-steps per tick, over a scripted input sequence. */
function simulate(from: State, inputs: readonly Input[]): State[] {
  const out: State[] = [];
  let s = from;
  for (const input of inputs) {
    for (let i = 0; i < STEPS_PER_TICK; i++) {
      s = step(s, input, g, params);
    }
    out.push(s);
  }
  return out;
}

function wire(s: State): WireState {
  const { x, y, vx, vy, on_ground, climbing, facing } = s;
  return { x, y, vx, vy, on_ground, climbing, facing };
}

/** Runs a predictor through whole ticks with consecutive sequence numbers from 1. */
function run(pred: Predictor, inputs: readonly Input[], firstSeq = 1): State[] {
  const out: State[] = [];
  inputs.forEach((input, i) => {
    pred.beginTick(firstSeq + i, input);
    for (let k = 0; k < STEPS_PER_TICK; k++) {
      pred.subStep();
    }
    out.push(pred.state);
  });
  return out;
}

const start = standing(spawn.x, spawn.y);
const script = [R, R, RJ, R, R, noInput, noInput, L, L, DJ, L, L, noInput, R, R, RJ, R, noInput];

describe('Predictor', () => {
  it('prediction over a scripted input sequence equals running sim/step directly', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    expect(run(pred, script)).toEqual(simulate(start, script));
  });

  it('reconcile with a server state equal to the prediction leaves the state untouched and drops the acked ticks', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    const states = run(pred, script);
    const before = pred.state;
    expect(pred.reconcile(wire(states[4]!), 5)).toBe('match');
    expect(pred.state).toBe(before);
    expect([...pred.history].map((e) => e.seq)).toEqual(
      script.map((_, i) => i + 1).filter((seq) => seq > 5),
    );
  });

  it('reconcile with a 5 px difference resets and replays, and the replayed state equals a fresh simulation from the server state', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    const states = run(pred, script);
    const server: WireState = { ...wire(states[4]!), x: states[4]!.x - 5 };
    expect(pred.reconcile(server, 5)).toBe('reset');
    const fresh = simulate(fromWire(server, g), script.slice(5));
    expect(pred.state).toEqual(fresh[fresh.length - 1]);
    expect([...pred.history].map((e) => e.state)).toEqual(fresh);
  });

  it('a 4 px difference is within tolerance and does not reset', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    const states = run(pred, script);
    expect(pred.reconcile({ ...wire(states[2]!), x: states[2]!.x + 4 }, 3)).toBe('match');
  });

  it('reconcile part way through a tick finishes the tick for the comparison', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    run(pred, [R, R]);
    pred.beginTick(3, R);
    pred.subStep(); // one of three sub-steps so far
    const whole = simulate(start, [R, R, R]);
    expect(pred.reconcile(wire(whole[2]!), 3)).toBe('match');
    pred.subStep();
    pred.subStep();
    expect(pred.state).toEqual(whole[2]);
  });

  it('reconcile with an ack_seq older than the buffer drops nothing and does not throw', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    const many = Array.from({ length: HISTORY_TICKS + 10 }, (_, i) => (i % 7 < 4 ? R : L));
    run(pred, many);
    expect(pred.history.size).toBe(HISTORY_TICKS);
    const oldest = [...pred.history][0]!.seq;
    expect(pred.reconcile(wire(start), oldest - 3)).toBe('stale');
    expect(pred.history.size).toBe(HISTORY_TICKS);
  });

  it('an ack_seq newer than every entry takes the server state as the truth', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    run(pred, [R, R, R]);
    const elsewhere = { ...wire(start), x: 100 };
    expect(pred.reconcile(elsewhere, 50)).toBe('ahead');
    expect(pred.state).toEqual(fromWire(elsewhere, g));
    expect(pred.history.size).toBe(0);
  });

  it('a Correction resets to its state and replays the inputs after its seq', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    const states = run(pred, script);
    const server = { ...wire(states[7]!), x: states[7]!.x + 30 };
    pred.correct(server, 8);
    const fresh = simulate(fromWire(server, g), script.slice(8));
    expect(pred.state).toEqual(fresh[fresh.length - 1]);
    expect([...pred.history].map((e) => e.seq)[0]).toBe(9);
  });

  it('server states computed by running sim/step 3 times per tick over the sent inputs never trigger a reset', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    let server = start;
    script.forEach((input, i) => {
      pred.beginTick(i + 1, input);
      for (let k = 0; k < STEPS_PER_TICK; k++) {
        pred.subStep();
      }
      for (let k = 0; k < STEPS_PER_TICK; k++) {
        server = step(server, input, g, params);
      }
      // the Delta for this tick arrives two ticks late
      if (i >= 2) {
        const acked = simulate(start, script.slice(0, i - 1));
        expect(pred.reconcile(wire(acked[acked.length - 1]!), i - 1)).toBe('match');
      }
    });
  });

  it('sub-steps before the first tick and past the third of a tick do nothing', () => {
    const pred = new Predictor(g);
    pred.reset(wire(start));
    pred.subStep();
    expect(pred.state).toEqual(fromWire(wire(start), g));
    pred.beginTick(1, R);
    for (let k = 0; k < 5; k++) {
      pred.subStep();
    }
    expect(pred.state).toEqual(simulate(start, [R])[0]);
  });
});

describe('fromWire', () => {
  const plat = g.platforms[0]!;
  it.each([
    {
      name: 'standing on a platform records its index',
      w: { ...wire(start), x: plat.x + plat.w / 2, y: plat.y, on_ground: true },
      ground: 0,
    },
    {
      name: 'standing on a solid records no platform',
      w: { ...wire(start), on_ground: true },
      ground: NO_PLATFORM,
    },
    {
      name: 'in the air records no platform even over one',
      w: { ...wire(start), x: plat.x + plat.w / 2, y: plat.y, on_ground: false },
      ground: NO_PLATFORM,
    },
    {
      name: 'at a platform height but beside it records no platform',
      w: { ...wire(start), x: plat.x + plat.w + params.width, y: plat.y, on_ground: true },
      ground: NO_PLATFORM,
    },
  ])('$name', ({ w, ground }) => {
    const s = fromWire(w, g);
    expect(s.ground).toBe(ground);
    expect(s.drop_platform).toBe(NO_PLATFORM);
    expect(s.drop_timer).toBe(0);
    expect(platformUnder(w.x, w.y, g, params)).toBe(
      w.on_ground ? ground : platformUnder(w.x, w.y, g, params),
    );
  });
});
