import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { tickTiming } from '@/lib/fixed-step';
import type { Input } from '@/sim/input';
import { parseMap } from '@/sim/map';
import { NO_PLATFORM, faster, params, standing, step } from '@/sim/step';
import type { State } from '@/sim/step';

import {
  HISTORY_MS,
  Predictor,
  fromWire,
  movementParams,
  platformUnder,
  wireState,
} from './prediction';
import type { WireState } from './prediction';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

const town = parseMap(
  'town',
  JSON.parse(readFileSync(path.resolve(process.cwd(), 'public/maps/town.tmj'), 'utf8')),
);
const g = town.geometry;
const spawn = town.player_spawn;
const TIMING = tickTiming(20, params.dt);
const STEPS = TIMING.stepsPerTick; // three 1/60 s steps a tick
const HISTORY_TICKS = HISTORY_MS / TIMING.tickMs;

const R: Input = { ...noInput, right: true };
const L: Input = { ...noInput, left: true };
const RJ: Input = { ...noInput, right: true, jump: true };
const DJ: Input = { ...noInput, down: true, jump: true };

function simulate(from: State, inputs: readonly Input[]): State[] {
  const out: State[] = [];
  let s = from;
  for (const input of inputs) {
    for (let i = 0; i < STEPS; i++) {
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

function run(pred: Predictor, inputs: readonly Input[], firstSeq = 1): State[] {
  const out: State[] = [];
  inputs.forEach((input, i) => {
    pred.beginTick(firstSeq + i, input);
    for (let k = 0; k < STEPS; k++) {
      pred.subStep();
    }
    out.push(pred.state);
  });
  return out;
}

const start = standing(spawn.x, spawn.y);
const script = [R, R, RJ, R, R, noInput, noInput, L, L, DJ, L, L, noInput, R, R, RJ, R, noInput];

describe('Predictor', () => {
  it('a tick holds as many steps as the world rate gives it', () => {
    for (const hz of [30, 12]) {
      const timing = tickTiming(hz, params.dt);
      const steps = timing.stepsPerTick;
      const pred = new Predictor(g, timing, params);
      pred.reset(wire(start));
      pred.beginTick(1, R);
      for (let k = 0; k < steps + 2; k++) {
        pred.subStep();
      }
      let s = start;
      for (let k = 0; k < steps; k++) {
        s = step(s, R, g, params);
      }
      expect(pred.state).toEqual(s);
    }
  });

  it('finishes a part-run tick with the world steps before comparing', () => {
    const pred = new Predictor(g, tickTiming(12, params.dt), params);
    pred.reset(wire(start));
    pred.beginTick(1, R);
    pred.subStep();
    let s = start;
    for (let k = 0; k < 5; k++) {
      s = step(s, R, g, params);
    }
    expect(pred.reconcile(wire(s), 1)).toBe('match');
  });

  it('prediction over a scripted input sequence equals running sim/step directly', () => {
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    expect(run(pred, script)).toEqual(simulate(start, script));
  });

  it('reconcile with a server state equal to the prediction leaves the state untouched and drops the acked ticks', () => {
    const pred = new Predictor(g, TIMING, params);
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
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    const states = run(pred, script);
    const server: WireState = { ...wire(states[4]!), x: states[4]!.x - 5 };
    expect(pred.reconcile(server, 5)).toBe('reset');
    const fresh = simulate(fromWire(server, g, params), script.slice(5));
    expect(pred.state).toEqual(fresh[fresh.length - 1]);
    expect([...pred.history].map((e) => e.state)).toEqual(fresh);
  });

  it('a 4 px difference is within tolerance and does not reset', () => {
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    const states = run(pred, script);
    expect(pred.reconcile({ ...wire(states[2]!), x: states[2]!.x + 4 }, 3)).toBe('match');
  });

  it('reconcile part way through a tick finishes the tick for the comparison', () => {
    const pred = new Predictor(g, TIMING, params);
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
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    const many = Array.from({ length: HISTORY_TICKS + 10 }, (_, i) => (i % 7 < 4 ? R : L));
    run(pred, many);
    expect(pred.history.size).toBe(HISTORY_TICKS);
    const oldest = [...pred.history][0]!.seq;
    expect(pred.reconcile(wire(start), oldest - 3)).toBe('stale');
    expect(pred.history.size).toBe(HISTORY_TICKS);
  });

  it('keeps two seconds of ticks at any rate', () => {
    for (const hz of [20, 60]) {
      const pred = new Predictor(g, tickTiming(hz, params.dt), params);
      pred.reset(wire(start));
      run(
        pred,
        Array.from({ length: 3 * hz }, () => R),
      );
      expect(pred.history.size).toBe(2 * hz);
    }
  });

  it('an ack_seq newer than every entry takes the server state as the truth', () => {
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    run(pred, [R, R, R]);
    const elsewhere = { ...wire(start), x: 100 };
    expect(pred.reconcile(elsewhere, 50)).toBe('ahead');
    expect(pred.state).toEqual(fromWire(elsewhere, g, params));
    expect(pred.history.size).toBe(0);
  });

  it('server states computed by running sim/step 3 times per tick over the sent inputs never trigger a reset', () => {
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    let server = start;
    script.forEach((input, i) => {
      pred.beginTick(i + 1, input);
      for (let k = 0; k < STEPS; k++) {
        pred.subStep();
      }
      for (let k = 0; k < STEPS; k++) {
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
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    pred.subStep();
    expect(pred.state).toEqual(fromWire(wire(start), g, params));
    pred.beginTick(1, R);
    for (let k = 0; k < 5; k++) {
      pred.subStep();
    }
    expect(pred.state).toEqual(simulate(start, [R])[0]);
  });
});

describe('move speed', () => {
  it('speeds a hunting map’s movement by 1 + move speed in thousandths, and leaves a town’s alone', () => {
    expect(movementParams('town', 0.3)).toBe(params);
    expect(movementParams('hunting', 0)).toBe(params);
    const p = movementParams('hunting', 0.0072);
    expect(p).toEqual(faster(params, 1.007));
    expect(p.walk_speed).toBeCloseTo(params.walk_speed * 1.007, 9);
  });

  it('the predictor walks at the new pace from the step after setParams', () => {
    const fast = movementParams('hunting', 0.3);
    const pred = new Predictor(g, TIMING, params);
    pred.reset(wire(start));
    run(pred, [R]);
    const before = pred.state;
    pred.setParams(fast);
    run(pred, [R, R], 2);
    let s = before;
    for (let k = 0; k < STEPS * 2; k++) {
      s = step(s, R, g, fast);
    }
    expect(pred.state).toEqual(s);
    expect(pred.state.x - before.x).toBeGreaterThan(simulate(before, [R, R]).at(-1)!.x - before.x);
  });
});

describe('wireState', () => {
  it.each([
    { facing: 1, want: 1 },
    { facing: -1, want: -1 },
    { facing: 0, want: 1 },
  ])('takes a body from the wire into sim terms, facing $facing as $want', ({ facing, want }) => {
    const view = { x: 1, y: 2, vx: 3, vy: 4, onGround: true, climbing: false, facing };
    expect(wireState(view)).toEqual({
      x: 1,
      y: 2,
      vx: 3,
      vy: 4,
      on_ground: true,
      climbing: false,
      facing: want,
    });
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
    const s = fromWire(w, g, params);
    expect(s.ground).toBe(ground);
    expect(s.drop_platform).toBe(NO_PLATFORM);
    expect(s.drop_timer).toBe(0);
    expect(platformUnder(w.x, w.y, g, params)).toBe(
      w.on_ground ? ground : platformUnder(w.x, w.y, g, params),
    );
  });
});
