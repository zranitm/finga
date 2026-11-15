import { describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';
import { tickTiming } from '@/lib/fixed-step';
import type { Input } from '@/sim/input';
import { faster, step } from '@/sim/step';
import type { Blocker, Geometry, State } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { HISTORY_MS, Predictor, RESET_DISTANCE_M, movementParams, wireState } from './prediction';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };
const params = movement;

/** 40 m square: flat, with a ramp rising east of x 24, a crate and a tree. */
const SIZE = 40;
const CELL = 2;
const N = SIZE / CELL + 1;
const heights = Array.from({ length: N * N }, (_, i) => Math.max(0, (i % N) * CELL - 24) * 0.4);
const g: Geometry = {
  size_x: SIZE,
  size_z: SIZE,
  terrain: terrain(CELL, N, N, heights, params.max_slope),
  boxes: [{ x: 14, z: 26, half_w: 1, half_d: 1, ux: 1, uz: 0, top: 1 }],
  cylinders: [{ x: 10, z: 14, radius: 0.6, top: 5 }],
};
const TIMING = tickTiming(20, params.dt);
const STEPS = TIMING.stepsPerTick; // three 1/60 s steps a tick
const HISTORY_TICKS = HISTORY_MS / TIMING.tickMs;

const R: Input = { ...noInput, right: true };
const L: Input = { ...noInput, left: true };
const U: Input = { ...noInput, up: true };
const UR: Input = { ...noInput, up: true, right: true };
const RJ: Input = { ...noInput, right: true, jump: true };
const DLJ: Input = { ...noInput, down: true, left: true, jump: true };
const noMobs = () => [];

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

function run(pred: Predictor, inputs: readonly Input[], firstSeq = 1): State[] {
  const out: State[] = [];
  inputs.forEach((input, i) => {
    pred.beginTick(firstSeq + i, input, 0);
    for (let k = 0; k < STEPS; k++) {
      pred.subStep();
    }
    out.push(pred.state);
  });
  return out;
}

const start: State = { x: 20, y: 0, z: 20, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 0, hz: 1 };
const script = [R, R, RJ, R, UR, UR, noInput, L, L, DLJ, L, U, U, noInput, R, R, RJ, R, noInput];

function predictor(): Predictor {
  const pred = new Predictor(g, TIMING, params, noMobs);
  pred.reset(start);
  return pred;
}

describe('Predictor', () => {
  it('a tick holds as many steps as the world rate gives it', () => {
    for (const hz of [30, 12]) {
      const timing = tickTiming(hz, params.dt);
      const steps = timing.stepsPerTick;
      const pred = new Predictor(g, timing, params, noMobs);
      pred.reset(start);
      pred.beginTick(1, R, 0);
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
    const pred = new Predictor(g, tickTiming(12, params.dt), params, noMobs);
    pred.reset(start);
    pred.beginTick(1, R, 0);
    pred.subStep();
    let s = start;
    for (let k = 0; k < 5; k++) {
      s = step(s, R, g, params);
    }
    expect(pred.reconcile(s, 1)).toBe('match');
  });

  it('prediction over a scripted input sequence, up the ramp, round the tree and jumping, equals running sim/step directly', () => {
    const states = run(predictor(), script);
    expect(states).toEqual(simulate(start, script));
    expect(Math.max(...states.map((s) => s.y))).toBeGreaterThan(0); // it left the ground
  });

  it('reconcile with a server state equal to the prediction leaves the state untouched and drops the acked ticks', () => {
    const pred = predictor();
    const states = run(pred, script);
    const before = pred.state;
    expect(pred.reconcile(states[4]!, 5)).toBe('match');
    expect(pred.state).toBe(before);
    expect([...pred.history].map((e) => e.seq)).toEqual(
      script.map((_, i) => i + 1).filter((seq) => seq > 5),
    );
  });

  it('reconcile with the world 0.15 m off resets and replays, and the replayed state equals a fresh simulation from the server state', () => {
    const pred = predictor();
    const states = run(pred, script);
    const server = { ...states[4]!, z: states[4]!.z - 0.15 };
    expect(pred.reconcile(server, 5)).toBe('reset');
    const fresh = simulate(server, script.slice(5));
    expect(pred.state).toEqual(fresh[fresh.length - 1]);
    expect([...pred.history].map((e) => e.state)).toEqual(fresh);
  });

  it.each([
    { axis: 'x', off: { x: 0.12 } },
    { axis: 'y', off: { y: 0.12 } },
    { axis: 'z', off: { z: -0.12 } },
  ])('the world $axis 0.12 m off is within the 0.125 m tolerance and does not reset', ({ off }) => {
    const pred = predictor();
    const s = run(pred, script)[2]!;
    expect(pred.reconcile({ ...s, ...offset(s, off) }, 3)).toBe('match');
  });

  it('a disagreement over the tolerance on any axis resets', () => {
    const pred = predictor();
    const s = run(pred, script)[2]!;
    const d = RESET_DISTANCE_M / Math.sqrt(3) + 0.001; // each axis inside it, all three past it
    expect(pred.reconcile({ ...s, x: s.x + d, y: s.y + d, z: s.z + d }, 3)).toBe('reset');
  });

  it('reconcile part way through a tick finishes the tick for the comparison', () => {
    const pred = predictor();
    run(pred, [R, R]);
    pred.beginTick(3, R, 0);
    pred.subStep(); // one of three sub-steps so far
    const whole = simulate(start, [R, R, R]);
    expect(pred.reconcile(whole[2]!, 3)).toBe('match');
    pred.subStep();
    pred.subStep();
    expect(pred.state).toEqual(whole[2]);
  });

  it('reconcile with an ack_seq older than the buffer drops nothing and does not throw', () => {
    const pred = predictor();
    const many = Array.from({ length: HISTORY_TICKS + 10 }, (_, i) => (i % 7 < 4 ? R : L));
    run(pred, many);
    expect(pred.history.size).toBe(HISTORY_TICKS);
    const oldest = [...pred.history][0]!.seq;
    expect(pred.reconcile(start, oldest - 3)).toBe('stale');
    expect(pred.history.size).toBe(HISTORY_TICKS);
  });

  it('keeps two seconds of ticks at any rate', () => {
    for (const hz of [20, 60]) {
      const pred = new Predictor(g, tickTiming(hz, params.dt), params, noMobs);
      pred.reset(start);
      run(
        pred,
        Array.from({ length: 3 * hz }, (_, i) => (i % 2 === 0 ? R : L)),
      );
      expect(pred.history.size).toBe(2 * hz);
    }
  });

  it('an ack_seq newer than every entry takes the server state as the truth', () => {
    const pred = predictor();
    run(pred, [R, R, R]);
    const elsewhere = { ...start, x: 5, z: 30 };
    expect(pred.reconcile(elsewhere, 50)).toBe('ahead');
    expect(pred.state).toEqual(elsewhere);
    expect(pred.history.size).toBe(0);
  });

  it('server states computed by running sim/step 3 times per tick over the sent inputs never trigger a reset', () => {
    const pred = predictor();
    script.forEach((input, i) => {
      pred.beginTick(i + 1, input, 0);
      for (let k = 0; k < STEPS; k++) {
        pred.subStep();
      }
      // the Delta for this tick arrives two ticks late
      if (i >= 2) {
        const acked = simulate(start, script.slice(0, i - 1));
        expect(pred.reconcile(acked[acked.length - 1]!, i - 1)).toBe('match');
      }
    });
  });

  it('sub-steps before the first tick and past the third of a tick do nothing', () => {
    const pred = predictor();
    pred.subStep();
    expect(pred.state).toEqual(start);
    pred.beginTick(1, R, 0);
    for (let k = 0; k < 5; k++) {
      pred.subStep();
    }
    expect(pred.state).toEqual(simulate(start, [R])[0]);
  });
});

describe('mobs', () => {
  const face = start.x + 1.5; // touched in the third tick at 0.25 m a tick
  const slime: Blocker = { x: face, z: start.z, radius: 0.5, bottom: 0, top: 0.875 };
  const atView = (view: number) => (view === 7 ? [slime] : []); // a slime 1.5 m east at tick 7, gone by 8
  const views = [7, 7, 7, 7, 7, 7, 8, 8];
  const touching = face - slime.radius - params.radius;

  /** What sim/step gives each tick walking east, blocked by the mobs at that tick's view. */
  function blocked(from: State, vs: readonly number[]): State[] {
    const out: State[] = [];
    let s = from;
    for (const view of vs) {
      for (let i = 0; i < STEPS; i++) {
        s = step(s, R, { ...g, blockers: atView(view) }, params);
      }
      out.push(s);
    }
    return out;
  }

  function walked(): Predictor {
    const pred = new Predictor(g, TIMING, params, atView);
    pred.reset(start);
    views.forEach((view, i) => {
      pred.beginTick(i + 1, R, view);
      for (let k = 0; k < STEPS; k++) {
        pred.subStep();
      }
    });
    return pred;
  }

  it('a mob at an input’s view stops the player at its side, and one gone from the view lets them walk on', () => {
    const pred = walked();
    const xs = [...pred.history].map((e) => e.state.x);
    for (const x of xs.slice(3, 6)) {
      expect(x).toBeCloseTo(touching, 9);
    }
    expect(pred.state.x).toBeGreaterThan(touching + 0.1);
    expect([...pred.history].map((e) => e.state)).toEqual(blocked(start, views));
  });

  it('a replay blocks each input against the mobs at the view it was predicted at', () => {
    const pred = walked();
    const first = [...pred.history][0]!.state;
    const server = { ...first, x: first.x - 0.2 };
    expect(pred.reconcile(server, 1)).toBe('reset');
    expect([...pred.history].map((e) => e.state)).toEqual(blocked(server, views.slice(1)));
  });
});

describe('move speed', () => {
  it('speeds a hunting map’s movement by 1 + move speed in thousandths, and leaves a town’s alone', () => {
    expect(movementParams('town', 0.3)).toBe(movement);
    expect(movementParams('hunting', 0)).toBe(movement);
    const p = movementParams('hunting', 0.0072);
    expect(p).toEqual(faster(movement, 1.007));
    expect(p.walk_speed).toBeCloseTo(movement.walk_speed * 1.007, 9);
  });

  it('the predictor walks at the new pace from the step after setParams', () => {
    const fast = movementParams('hunting', 0.3);
    const pred = predictor();
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

  it('a buff’s pace walks faster from the first input past its seq, and a replay runs each tick at the pace it ran at', () => {
    const fast = movementParams('hunting', 0.3);
    const pred = predictor();
    const states = run(pred, [R, R]);
    pred.pace(2, () => fast);
    run(pred, [R, R], 3);
    let s = states[1]!;
    for (let k = 0; k < STEPS * 2; k++) {
      s = step(s, R, g, fast);
    }
    expect(pred.state).toEqual(s);
    const server = { ...states[0]!, x: states[0]!.x - 0.2 };
    expect(pred.reconcile(server, 1)).toBe('reset');
    let r = server;
    for (let k = 0; k < STEPS * 3; k++) {
      r = step(r, R, g, k < STEPS ? params : fast);
    }
    expect(pred.state).toEqual(r);
  });

  it('a refused buff’s pace goes: waiting, it never applies; applied, its ticks and those since replay at the pace it replaced', () => {
    const fast = movementParams('hunting', 0.3);
    const pred = predictor();
    run(pred, [R]);
    pred.dropPace(pred.pace(1, () => fast));
    expect(run(pred, [R], 2)[0]?.vx).toBe(params.walk_speed);

    pred.reset(start);
    const states = run(pred, [R, R]);
    expect(pred.reconcile(states[0]!, 1)).toBe('match');
    const refused = pred.pace(2, () => fast);
    run(pred, [R, R], 3);
    expect(pred.state.vx).toBe(fast.walk_speed);
    pred.dropPace(refused);
    expect(pred.state).toEqual(simulate(states[0]!, [R, R, R]).at(-1));
    expect(run(pred, [R], 5)[0]?.vx).toBe(params.walk_speed);
  });

  it('a reset, for a new map or a rejoin, leaves no pace waiting', () => {
    const pred = predictor();
    run(pred, [R]);
    pred.pace(1, () => movementParams('hunting', 0.3));
    pred.reset(start);
    expect(run(pred, [R], 2)[0]?.vx).toBe(params.walk_speed);
  });

  it('a pace reads its params as it applies, so it follows a sheet that came since it was set', () => {
    let speed = 0.3;
    const pred = predictor();
    run(pred, [R]);
    pred.pace(1, () => movementParams('hunting', speed));
    speed = 0.1;
    expect(run(pred, [R], 2)[0]?.vx).toBe(movementParams('hunting', 0.1).walk_speed);
  });

  it('two applied paces dropped last first give back the pace before both', () => {
    const pred = predictor();
    const states = run(pred, [R]);
    expect(pred.reconcile(states[0]!, 1)).toBe('match');
    const hit = pred.pace(1, () => movementParams('hunting', 0.3));
    const end = pred.pace(2, () => movementParams('hunting', 0.1));
    run(pred, [R, R], 2);
    pred.dropPace(end);
    pred.dropPace(hit);
    expect(run(pred, [R], 4)[0]?.vx).toBe(params.walk_speed);
  });
});

describe('wireState', () => {
  it('takes a body from the wire into sim terms', () => {
    const view = { x: 1, y: 2, z: 3, vx: 4, vy: 5, vz: 6, onGround: true, hx: 0, hz: -1 };
    expect(wireState(view)).toEqual({
      x: 1,
      y: 2,
      z: 3,
      vx: 4,
      vy: 5,
      vz: 6,
      on_ground: true,
      hx: 0,
      hz: -1,
    });
  });
});

function offset(s: State, off: Partial<Record<'x' | 'y' | 'z', number>>): Partial<State> {
  return {
    x: s.x + (off.x ?? 0),
    y: s.y + (off.y ?? 0),
    z: s.z + (off.z ?? 0),
  };
}
