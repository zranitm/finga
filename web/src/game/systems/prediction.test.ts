import { describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';
import { tickTiming } from '@/lib/fixed-step';
import type { Input } from '@/sim/input';
import { faster, step } from '@/sim/step';
import type { Blocker, Geometry, State } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { HISTORY_MS, Predictor, movementParams, wireState } from './prediction';

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
const TICK = 100; // the world tick a reconcile's Delta came at, which only matters near mobs

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
    pred.reconcile(s, 1, TICK);
    expect(pred.resets).toBe(0);
  });

  it('prediction over a scripted input sequence, up the ramp, round the tree and jumping, equals running sim/step directly', () => {
    const states = run(predictor(), script);
    expect(states).toEqual(simulate(start, script));
    expect(Math.max(...states.map((s) => s.y))).toBeGreaterThan(0); // it left the ground
  });

  it('reconcile with a server state equal to the prediction leaves the state untouched and lets the acked ticks go', () => {
    const pred = predictor();
    const states = run(pred, script);
    const before = pred.state;
    pred.reconcile(states[4]!, 5, TICK);
    expect([pred.state, pred.resets]).toEqual([before, 0]);
    pred.reconcile(nudged(states[2]!, 'x'), 3, TICK); // acked already: a stale Delta
    expect([pred.state, pred.resets]).toEqual([before, 0]);
  });

  it('reconcile with the world off by a hair resets and replays, the replay a fresh simulation from the server state', () => {
    const pred = predictor();
    const states = run(pred, script);
    const server = nudged(states[4]!, 'z');
    pred.reconcile(server, 5, TICK);
    expect([pred.state, pred.resets]).toEqual([simulate(server, script.slice(5)).at(-1), 1]);
  });

  it.each(['x', 'y', 'z', 'vx', 'vy', 'vz', 'on_ground', 'hx', 'hz'] as const)(
    'a world different in %s alone resets: the sims run alike, so any difference is real',
    (field) => {
      const pred = predictor();
      const s = run(pred, script)[2]!;
      pred.reconcile(nudged(s, field), 3, TICK);
      expect(pred.resets).toBe(1);
    },
  );

  it('reconcile part way through a tick finishes the tick for the comparison', () => {
    const pred = predictor();
    run(pred, [R, R]);
    pred.beginTick(3, R, 0);
    pred.subStep(); // one of three sub-steps so far
    const whole = simulate(start, [R, R, R]);
    pred.reconcile(whole[2]!, 3, TICK);
    expect(pred.resets).toBe(0);
    pred.subStep();
    pred.subStep();
    expect(pred.state).toEqual(whole[2]);
  });

  it('a reset part way through a tick takes the world’s whole tick and steps it no further', () => {
    const pred = predictor();
    run(pred, [R, R]);
    pred.beginTick(3, R, 0);
    pred.subStep();
    const server = nudged(simulate(start, [R, R, R])[2]!, 'x');
    pred.reconcile(server, 3, TICK);
    pred.subStep();
    pred.subStep();
    expect([pred.state, pred.resets]).toEqual([server, 1]);
  });

  it('reconcile with an ack_seq older than the buffer changes nothing', () => {
    const pred = predictor();
    const many = Array.from({ length: HISTORY_TICKS + 10 }, (_, i) => (i % 7 < 4 ? R : L));
    const states = run(pred, many);
    const before = pred.state;
    pred.reconcile(nudged(states[7]!, 'x'), 8, TICK);
    expect([pred.state, pred.resets]).toEqual([before, 0]);
    pred.reconcile(nudged(states[10]!, 'x'), 11, TICK); // the oldest kept
    expect(pred.resets).toBe(1);
  });

  it('keeps two seconds of ticks at any rate', () => {
    for (const hz of [20, 60]) {
      const pred = new Predictor(g, tickTiming(hz, params.dt), params, noMobs);
      pred.reset(start);
      const states = run(
        pred,
        Array.from({ length: 3 * hz }, (_, i) => (i % 2 === 0 ? R : L)),
      );
      pred.reconcile(nudged(states[hz - 1]!, 'x'), hz, TICK); // let go
      expect(pred.resets).toBe(0);
      pred.reconcile(nudged(states[hz]!, 'x'), hz + 1, TICK); // kept
      expect(pred.resets).toBe(1);
    }
  });

  it('an ack_seq newer than every entry takes the server state as the truth', () => {
    const pred = predictor();
    run(pred, [R, R, R]);
    const elsewhere = { ...start, x: 5, z: 30 };
    pred.reconcile(elsewhere, 50, TICK);
    expect([pred.state, pred.resets]).toEqual([elsewhere, 1]);
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
        pred.reconcile(acked[acked.length - 1]!, i - 1, TICK + i - 1);
      }
    });
    expect(pred.resets).toBe(0);
  });

  it('each input the world drains costs one reset, and the frames after it agree again', () => {
    const pred = predictor();
    const inputs = Array.from({ length: 16 }, () => R);
    const drained = new Set([6, 8]); // the oldest goes and the next steps, a tick apart
    const stepped = (ack: number) => inputs.slice(0, ack).filter((_, k) => !drained.has(k + 1));
    inputs.forEach((input, i) => {
      pred.beginTick(i + 1, input, 0);
      for (let k = 0; k < STEPS; k++) {
        pred.subStep();
      }
      const ack = i - 1; // each Delta two ticks late, none for a drained input
      if (ack >= 1 && !drained.has(ack)) {
        pred.reconcile(simulate(start, stepped(ack)).at(-1)!, ack, TICK + ack);
      }
    });
    expect(pred.resets).toBe(2);
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

  /** Walks east a tick per view, giving the state after each. */
  function walked(pred: Predictor): State[] {
    return views.map((view, i) => {
      pred.beginTick(i + 1, R, view);
      for (let k = 0; k < STEPS; k++) {
        pred.subStep();
      }
      return pred.state;
    });
  }

  function predictorAmong(blockersAt: (view: number) => Blocker[]): Predictor {
    const pred = new Predictor(g, TIMING, params, blockersAt);
    pred.reset(start);
    return pred;
  }

  it('a mob at an input’s view stops the player at its side, and one gone from the view lets them walk on', () => {
    const states = walked(predictorAmong(atView));
    for (const s of states.slice(3, 6)) {
      expect(s.x).toBeCloseTo(touching, 9);
    }
    expect(states.at(-1)!.x).toBeGreaterThan(touching + 0.1);
    expect(states).toEqual(blocked(start, views));
  });

  it('a replay blocks each input against the mobs at the view it was predicted at', () => {
    const pred = predictorAmong(atView);
    const first = walked(pred)[0]!;
    const server = { ...first, x: first.x - 0.2 };
    pred.reconcile(server, 1, 9); // the later inputs run at ticks 10 to 16, their views within the 8 frames kept
    expect([pred.state, pred.resets]).toEqual([blocked(server, views.slice(1)).at(-1), 1]);
  });

  it('holds an input’s view within the 8 frames the world keeps at the tick it runs, one input a tick from the last ack, as the world does', () => {
    const asked: number[] = [];
    const pred = predictorAmong((view) => {
      asked.push(view);
      return [];
    });
    pred.beginTick(1, R, 3);
    expect(asked).toEqual([3]); // no ack yet: the view as drawn
    pred.subStep();
    pred.reconcile(simulate(start, [R])[0]!, 1, 20);
    asked.length = 0;
    pred.beginTick(2, R, 3); // runs at 21: no older than 13
    pred.beginTick(3, R, 99); // runs at 22: no newer than 21
    pred.beginTick(4, R, 18);
    expect(asked).toEqual([13, 21, 18]);
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
    pred.reconcile(server, 1, TICK);
    expect(pred.resets).toBe(1);
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
    pred.reconcile(states[0]!, 1, TICK);
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
    pred.reconcile(states[0]!, 1, TICK);
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

/** s with field moved the least it can be: a bit, or on_ground flipped. */
function nudged(s: State, field: keyof State): State {
  const v = s[field];
  return {
    ...s,
    [field]: typeof v === 'boolean' ? !v : v + Math.max(Math.abs(v), 1) * Number.EPSILON,
  };
}
