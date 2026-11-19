import { describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';
import { tickTiming } from '@/lib/fixed-step';
import type { Input } from '@/sim/input';
import { blink, dash, endSprint, faster, fighting, sprintSteps, step } from '@/sim/step';
import type { Blocker, Geometry, Params, State } from '@/sim/step';
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

/** The world's states for inputs walked from the west of the map, input k running at tick 100 + k, stepped with no sprint at the ticks fightsAt says. */
function worldWalk(inputs: readonly Input[], fightsAt: (tick: number) => boolean): State[] {
  const out: State[] = [];
  let s = west;
  inputs.forEach((input, i) => {
    const p = fightsAt(101 + i) ? fighting(params) : params;
    for (let k = 0; k < STEPS; k++) {
      s = step(s, input, g, p);
    }
    out.push(s);
  });
  return out;
}

const west: State = { ...start, x: 2, z: 4 }; // a clear run east, short of the ramp
const SPRINT_TICKS = sprintSteps(params) / STEPS; // 40: a sprint from the 41st input walked

describe('Predictor in combat (large-regions.md section 7)', () => {
  function walking(): Predictor {
    const pred = new Predictor(g, TIMING, params, noMobs);
    pred.reset(west);
    return pred;
  }

  /** Walks east inputs from to through to, each acked with the world's state as it runs, the reconcile finding the prediction the same or resetting; the predicted states. */
  function acked(pred: Predictor, world: readonly State[], from: number, to: number): State[] {
    const out: State[] = [];
    for (let k = from; k <= to; k++) {
      out.push(...run(pred, [R], k));
      pred.reconcile(world[k - 1]!, k, 100 + k);
    }
    return out;
  }

  it('a hit while sprinting: from the tick after it the world steps with no sprint, and only the ticks predicted since replay so, never the sprint before', () => {
    const pred = walking();
    const walk = Array<Input>(45).fill(R);
    const calm = worldWalk(walk, () => false);
    acked(pred, calm, 1, 41); // sprinting from input 41
    run(pred, walk.slice(41), 42); // 42 to 45 still on their way
    const sprinting = pred.state;
    expect([sprinting, sprinting.sprint]).toEqual([calm[44], sprintSteps(params)]);
    pred.fought(142, 100); // a mob's hit at tick 142, after input 42 ran: in combat from 43
    const hit = worldWalk(walk, (tick) => tick > 142);
    expect(pred.state).toEqual(hit[44]);
    const sprintedOn =
      3 * STEPS * params.dt * (params.walk_speed * params.sprint.speed - params.walk_speed);
    expect(sprinting.x - pred.state.x).toBeCloseTo(sprintedOn, 9); // inputs 43 to 45 alone
    for (let k = 42; k <= 45; k++) {
      pred.reconcile(hit[k - 1]!, k, 100 + k);
    }
    expect(pred.resets).toBe(0); // the world agrees as it runs them
  });

  it('a reconcile part way through a tick in combat finishes it with no sprint, as the world runs it', () => {
    const pred = walking();
    const walk = Array<Input>(43).fill(R);
    const world = worldWalk(walk, (tick) => tick > 142);
    acked(pred, world, 1, 42); // sprinting from input 41
    pred.fought(142, 100);
    pred.beginTick(43, R, 0);
    pred.subStep(); // one of its three
    pred.reconcile(world[42]!, 43, 143);
    expect(pred.resets).toBe(0);
  });

  it('out of combat again, the count starts on the same sub-step as the world’s', () => {
    const pred = walking();
    const walk = Array<Input>(SPRINT_TICKS + 20).fill(R);
    const world = worldWalk(walk, (tick) => tick > 105 && tick < 115); // a hit at 105 with a fight of 10 ticks: in it from 106 until 115
    acked(pred, world, 1, 1);
    pred.fought(105, 10);
    const predicted = acked(pred, world, 2, walk.length);
    expect(pred.resets).toBe(0);
    expect(predicted.map((s) => s.sprint ?? 0).slice(12, 15)).toEqual([0, 3, 6]); // inputs 14, 15, 16: the count from 15's tick, 115
    expect(predicted.at(-1)!.sprint).toBe(sprintSteps(params)); // sprinting 2 s on
  });

  it('a second hit inside the fight keeps it from the first, to its own end', () => {
    const pred = walking();
    const walk = Array<Input>(30).fill(R);
    const world = worldWalk(walk, (tick) => tick > 103 && tick < 120);
    acked(pred, world, 1, 1);
    pred.fought(103, 10);
    pred.fought(110, 10); // in the fight still: until 120
    expect(
      acked(pred, world, 2, walk.length)
        .map((s) => s.sprint ?? 0)
        .slice(16, 20),
    ).toEqual([0, 0, 3, 6]); // inputs 18 to 21: counting from 20's tick, 120
    expect(pred.resets).toBe(0);
  });

  it('a rejoin is a new player, out of combat', () => {
    const pred = walking();
    const walk = Array<Input>(10).fill(R);
    const world = worldWalk(walk, () => false);
    acked(pred, world, 1, 1);
    pred.fought(101, 100);
    pred.endFight();
    acked(pred, world, 2, walk.length);
    expect([pred.state.sprint, pred.resets]).toEqual([30, 0]); // all 10 inputs counted
  });

  it('a press ends the count from the first input past it, as the world ends it', () => {
    const pred = walking();
    run(pred, Array<Input>(SPRINT_TICKS + 5).fill(R));
    pred.mark(SPRINT_TICKS + 5, endSprint);
    run(pred, [R, R], SPRINT_TICKS + 6);
    expect(pred.state.sprint).toBe(6); // the input past it counts afresh, and the next
  });
});

describe('Predictor', () => {
  it('says where the tick being run leaves the player, part way through it too, without moving them', () => {
    const pred = predictor();
    expect(pred.tickEnd()).toEqual(start); // before any tick
    run(pred, [R, R]);
    pred.beginTick(3, U, 0);
    pred.subStep(); // one of its three steps
    const now = pred.state;
    expect(pred.tickEnd()).toEqual(simulate(start, [R, R, U]).at(-1));
    expect(pred.state).toBe(now);
    pred.subStep();
    pred.subStep();
    expect(pred.tickEnd()).toEqual(pred.state); // its steps all run
  });

  it('says the server tick the drawn player stands at: its input’s, one a tick from the last ack', () => {
    const pred = predictor();
    run(pred, [R, R, R]); // inputs 1 to 3
    expect(pred.tickAt(1)).toBeNull(); // nothing acknowledged yet
    pred.reconcile(simulate(start, [R])[0]!, 1, 40); // input 1 ran at tick 40, so 3 runs at 42
    expect(pred.tickAt(1)).toBe(42); // its whole tick stepped and drawn: the state the world has after tick 42
    pred.beginTick(4, R, 0);
    pred.subStep();
    expect(pred.tickAt(0)).toBe(42); // drawn at the step before its first: still 42's end
    expect(pred.tickAt(0.5)).toBeCloseTo(42 + 0.5 / STEPS, 9);
  });

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

  it.each([{ held: 9 }, { slow: { left: 30, factor: 0.6 } }, { sprint: 7 }])(
    'a world that differs in %o alone resets, and the prediction steps on from it as the world will',
    (status) => {
      const pred = predictor();
      run(pred, script.slice(0, 5));
      const server = { ...simulate(start, script.slice(0, 3))[2]!, ...status };
      pred.reconcile(server, 3, TICK);
      expect([pred.state, pred.resets]).toEqual([simulate(server, script.slice(3, 5)).at(-1), 1]);
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
  it('speeds a hunting or match map’s movement by 1 + move speed in thousandths, and leaves a town’s alone', () => {
    expect(movementParams('town', 0.3)).toBe(movement);
    expect(movementParams('hunting', 0)).toBe(movement);
    const p = movementParams('hunting', 0.0072);
    expect(p).toEqual(faster(movement, 1.007));
    expect(p.walk_speed).toBeCloseTo(movement.walk_speed * 1.007, 9);
    expect(movementParams('pvp', 0.0072)).toEqual(p);
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

describe('a skill’s move', () => {
  const north = { x: 0, z: 1, length: 4 };
  const dashNorth = (s: State, _g: Geometry, p: Params) => dash(s, north, 12, 'stop', false, p);
  const blinkNorth = (s: State, at: Geometry, p: Params) => blink(s, north, at, p);

  /** from run on by inputs, the move started at the first: as the world does it, from the input past the press. */
  function moved(from: State, startMove: typeof dashNorth, inputs: readonly Input[]): State[] {
    const out: State[] = [];
    let s = startMove(from, g, params);
    for (const input of inputs) {
      for (let i = 0; i < STEPS; i++) {
        s = step(s, input, g, params);
      }
      out.push(s);
    }
    return out;
  }

  it('starts from the first input past its press, as the world starts it', () => {
    const pred = predictor();
    const before = run(pred, [R, R]);
    pred.mark(2, dashNorth);
    const after = run(pred, [R, R, R, R, R], 3);
    expect(after).toEqual(moved(before[1]!, dashNorth, [R, R, R, R, R]));
    expect(after[3]!.move).toBeUndefined(); // 12 sub-steps: done in four inputs
  });

  it('a world whose leap through mobs reached a different clear point resets: that point is where the leap ends if it runs out inside a body', () => {
    const leapNorth = (s: State, _g: Geometry, p: Params) => dash(s, north, 12, 'through', true, p);
    const pred = predictor();
    run(pred, [R, R]);
    pred.mark(2, leapNorth);
    const [mid] = run(pred, [R], 3);
    expect(mid!.move?.through).toBe(true);
    pred.reconcile(
      { ...mid!, move: { ...mid!.move!, clear_x: mid!.move!.clear_x! + 0.25 } },
      3,
      TICK,
    );
    expect(pred.resets).toBe(1);
  });

  it('a mark during an input not begun yet waits for the input after it', () => {
    const pred = predictor();
    const before = run(pred, [R, R]);
    pred.mark(3, dashNorth); // pressed during input 3, before it began
    const third = run(pred, [R], 3);
    expect(third[0]).toEqual(simulate(before[1]!, [R])[0]);
    expect(run(pred, [R], 4)[0]).toEqual(moved(third[0]!, dashNorth, [R])[0]);
  });

  it('a mark on a body a status holds starts no move, as the world starts none', () => {
    const pred = predictor();
    run(pred, [R, R]);
    const held = { ...simulate(start, [R, R])[1]!, held: 30 };
    pred.reconcile(held, 2, TICK);
    pred.mark(2, blinkNorth);
    expect(run(pred, [R, R], 3)).toEqual(simulate(held, [R, R]));
  });

  it('a break-free’s mark ends the hold first, at the same input, and its blink lands from where the body was held; a world frame telling of the hold only later replays it so', () => {
    const held = { ...simulate(start, [R, R])[1]!, held: 30 };
    const want = moved({ ...held, held: 0 }, blinkNorth, [U, U]);
    const told = predictor();
    run(told, [R, R]);
    told.reconcile(held, 2, TICK);
    told.mark(2, blinkNorth, true);
    expect(run(told, [U, U], 3)).toEqual(want);
    const late = predictor();
    run(late, [R, R]);
    late.mark(2, blinkNorth, true);
    run(late, [U, U], 3);
    late.reconcile(held, 2, TICK);
    expect([late.state, late.resets]).toEqual([want.at(-1), 1]);
  });

  it('a break-free with no move still ends the hold at its input: the body walks from it', () => {
    const pred = predictor();
    run(pred, [R, R]);
    const held = { ...simulate(start, [R, R])[1]!, held: 30 };
    pred.reconcile(held, 2, TICK);
    pred.mark(2, (s) => s, true);
    const walked = run(pred, [U], 3)[0]!;
    expect(walked).toEqual(simulate({ ...held, held: 0 }, [U])[0]);
    expect(walked.z).toBeGreaterThan(held.z);
  });

  it('a reconcile replays a blink where it landed, at the start of its input, from the world’s state', () => {
    const pred = predictor();
    run(pred, [R, R]);
    pred.mark(2, blinkNorth);
    run(pred, [U, U, U], 3);
    const server = nudged(simulate(start, [R, R])[1]!, 'x');
    pred.reconcile(server, 2, TICK);
    expect([pred.state, pred.resets]).toEqual([moved(server, blinkNorth, [U, U, U]).at(-1), 1]);
  });

  it('an ack past the history keeps a mark still waiting for its input', () => {
    const pred = predictor();
    run(pred, [R]);
    pred.mark(5, dashNorth);
    const server = simulate(start, [R, R, R, R])[3]!;
    pred.reconcile(server, 4, TICK); // newer than every entry: the world's state as the truth
    run(pred, [R], 5);
    expect(run(pred, [R], 6)[0]).toEqual(moved(simulate(server, [R])[0]!, dashNorth, [R])[0]);
  });

  it('a refused skill’s mark goes: waiting, it never starts; started, its ticks replay without it', () => {
    const waiting = predictor();
    run(waiting, [R, R]);
    waiting.dropMark(waiting.mark(2, dashNorth));
    expect(run(waiting, [R, R], 3)).toEqual(simulate(start, [R, R, R, R]).slice(2));

    const started = predictor();
    run(started, [R, R]);
    const m = started.mark(2, dashNorth);
    run(started, [R, R], 3);
    started.reconcile(simulate(start, [R])[0]!, 1, TICK);
    started.dropMark(m);
    expect(started.state).toEqual(simulate(start, [R, R, R, R]).at(-1));
  });

  it('a world with a move the client didn’t start resets', () => {
    const pred = predictor();
    const s = run(pred, script)[2]!;
    pred.reconcile({ ...s, move: { vx: 0, vz: 20, left: 3, stop: true } }, 3, TICK);
    expect(pred.resets).toBe(1);
  });

  it.each(['vx', 'vz', 'left', 'stop'] as const)(
    'a world whose move differs in %s alone resets',
    (field) => {
      const pred = predictor();
      run(pred, [R]);
      pred.mark(1, dashNorth);
      const s = run(pred, [R], 2)[0]!;
      const m = s.move!;
      const v = m[field];
      pred.reconcile(
        { ...s, move: { ...m, [field]: typeof v === 'boolean' ? !v : v + 1 } },
        2,
        TICK,
      );
      expect(pred.resets).toBe(1);
    },
  );

  it('a reset, for a new map or a rejoin, leaves no mark waiting', () => {
    const pred = predictor();
    run(pred, [R]);
    pred.mark(1, dashNorth);
    pred.reset(start);
    expect(run(pred, [R], 2)).toEqual(simulate(start, [R]));
  });
});

describe('wireState', () => {
  it('takes a move under way from the wire, and leaves out none', () => {
    const view = { x: 1, y: 2, z: 3, vx: 4, vy: 5, vz: 6, onGround: true, hx: 0, hz: -1 };
    const none = { through: false, clearX: 0, clearZ: 0 };
    expect(
      wireState({ ...view, move: { vx: 0, vz: 20, left: 7, stop: true, ...none } }).move,
    ).toEqual({
      vx: 0,
      vz: 20,
      left: 7,
      stop: true,
    });
    expect(
      wireState({ ...view, move: { vx: 0, vz: 0, left: 0, stop: false, ...none } }),
    ).not.toHaveProperty('move');
  });

  it('takes a move passing through mobs with the farthest point it reached clear of them, as the sim keeps it', () => {
    const view = { x: 1, y: 2, z: 3, vx: 4, vy: 5, vz: 6, onGround: true, hx: 0, hz: -1 };
    const leap = { vx: -13, vz: 0, left: 5, stop: false, through: true, clearX: 6.5, clearZ: 3 };
    expect(wireState({ ...view, move: leap }).move).toEqual({
      vx: -13,
      vz: 0,
      left: 5,
      stop: false,
      through: true,
      clear_x: 6.5,
      clear_z: 3,
    });
  });

  it('takes a hold and a slow from the wire, and leaves out none', () => {
    const view = { x: 1, y: 2, z: 3, vx: 4, vy: 5, vz: 6, onGround: true, hx: 0, hz: -1 };
    expect(wireState({ ...view, held: 12, slow: { left: 30, factor: 0.6 } })).toMatchObject({
      held: 12,
      slow: { left: 30, factor: 0.6 },
    });
    const none = wireState({ ...view, held: 0, slow: undefined });
    expect(none).not.toHaveProperty('held');
    expect(none).not.toHaveProperty('slow');
  });

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
function nudged(s: State, field: Exclude<keyof State, 'move' | 'held' | 'slow' | 'sprint'>): State {
  const v = s[field];
  return {
    ...s,
    [field]: typeof v === 'boolean' ? !v : v + Math.max(Math.abs(v), 1) * Number.EPSILON,
  };
}
