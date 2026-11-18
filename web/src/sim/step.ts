// Mirrors server/internal/shared/sim/sim.go op for op, so testdata/movement replays on both: metres, x east, y up, z north, no trigonometry.
import type { Input } from './input';
import { height, steep, top } from './terrain';
import type { Terrain } from './terrain';

/** √½, a diagonal heading's share on each axis, written out so no trigonometry is needed. */
export const DIAGONAL = 0.7071067811865476;

export interface Params {
  readonly dt: number; // one sub-step, 1/60 s
  readonly walk_speed: number; // m/s
  readonly jump_velocity: number; // m/s, upward
  readonly gravity: number; // m/s²
  readonly max_fall_speed: number; // m/s
  readonly radius: number; // the body's cylinder
  readonly height: number;
  readonly step: number; // a solid's top up to this far above the feet is stepped onto
  readonly max_slope: number; // a rise per metre; a cell rising more is steep
  readonly snap: number; // the most the ground may fall away under a walking body before it falls
  /** How walking out of combat breaks into a sprint; absent never sprints, as in combat. */
  readonly sprint?: Sprint;
}

/** After after seconds of walking out of combat, the walk runs speed times as fast (large-regions.md section 7). */
export interface Sprint {
  readonly after: number; // s
  readonly speed: number; // × the walk speed
}

/** p as a body in combat moves, as Go's Params.Fighting: it never sprints. */
export function fighting(p: Params): Params {
  const { sprint, ...rest } = p;
  return sprint ? rest : p;
}

/** How many sub-steps of walking break into a sprint, 0 for none, as Go's SprintSteps. */
export function sprintSteps(p: Params): number {
  return p.sprint && p.sprint.after > 0 ? Math.round(p.sprint.after / p.dt) : 0;
}

/** s with its sprint's count ended, as an attack or a skill pressed ends it. */
export function endSprint(s: State): State {
  const { sprint, ...rest } = s;
  return sprint ? rest : s;
}

/** 1 plus a move speed, rounded to thousandths as Go's SpeedFactor rounds it, so both multiply by the same number. */
export function speedFactor(moveSpeed: number): number {
  return Math.round((1 + moveSpeed) * 1000) / 1000;
}

/** p played f times as fast, as the server's Params.Faster: speeds by f and gravity by f², so a jump takes 1/f of the time. */
export function faster(p: Params, f: number): Params {
  if (f === 1) {
    return p;
  }
  return {
    ...p,
    walk_speed: p.walk_speed * f,
    jump_velocity: p.jump_velocity * f,
    max_fall_speed: p.max_fall_speed * f,
    gravity: p.gravity * (f * f),
  };
}

/** An upright box up to top, centred at (x, z), half_w along the unit axis (ux, uz) and half_d across it; axis-aligned is (1, 0). */
export interface Box {
  readonly x: number;
  readonly z: number;
  readonly half_w: number;
  readonly half_d: number;
  readonly ux: number;
  readonly uz: number;
  readonly top: number;
}

/** An upright cylinder up to top. */
export interface Cylinder {
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly top: number;
}

/** A mob's circle from bottom to top: a body overlapping it may not move its centre closer. */
export interface Blocker {
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly bottom: number;
  readonly top: number;
}

/** A map's ground and solids, size_x by size_z metres with walls at the edges, and the mobs the caller sets per step. */
export interface Geometry {
  readonly size_x: number;
  readonly size_z: number;
  readonly terrain: Terrain;
  readonly boxes: readonly Box[];
  readonly cylinders: readonly Cylinder[];
  readonly blockers?: readonly Blocker[];
}

/** A body's movement state, (x, y, z) at the centre of its feet, y up, facing the unit heading (hx, hz). */
export interface State {
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly vx: number;
  readonly vy: number;
  readonly vz: number;
  readonly on_ground: boolean;
  readonly hx: number;
  readonly hz: number;
  /** A skill's dash or leap under way; absent when none, as the server leaves it out. */
  readonly move?: Move;
  /** Sub-steps left of a status holding the body: no keys, no jump, no skill's move, gravity still running; absent when none. */
  readonly held?: number;
  /** A status slowing the walk; absent when none. */
  readonly slow?: Slow;
  /** Sub-steps walked toward a sprint, held at sprintSteps once there: from the next, it sprints; absent when none. */
  readonly sprint?: number;
}

/** A status slowing the walk: factor × the walk speed for left more sub-steps; a skill's move keeps its speed. */
export interface Slow {
  readonly left: number;
  readonly factor: number;
}

const NO_KEYS: Input = { left: false, right: false, up: false, down: false, jump: false };

/** A skill's dash or leap under way: (vx, vz) replaces the walk for left more sub-steps. With stop it ends at a mob's body; with through it passes mobs, and if it runs out inside one it ends at (clear_x, clear_z), the farthest point it reached that no mob's body held. Those three are absent otherwise, as the server leaves them out. */
export interface Move {
  readonly vx: number;
  readonly vz: number;
  readonly left: number;
  readonly stop: boolean;
  readonly through?: boolean;
  readonly clear_x?: number;
  readonly clear_z?: number;
}

/** How a dash or leap meets mobs' bodies, as the server's Mobs: stopped by one along the way it pushes, sliding along it; ended at the first it meets; or passing through them, to end clear of every body. */
export type Mobs = 'slide' | 'stop' | 'through';

/** A skill's move: along the unit (x, z) for length metres. */
export interface Way {
  readonly x: number;
  readonly z: number;
  readonly length: number;
}

/** Starts a skill's dash along w over n sub-steps, meeting mobs as m says, as the server's Dash; a leap also hops, so as to land on flat ground as it ends. */
export function dash(s: State, w: Way, n: number, m: Mobs, leap: boolean, p: Params): State {
  if (n < 1) {
    return s;
  }
  const speed = w.length / (n * p.dt);
  const move: Move = {
    vx: w.x * speed,
    vz: w.z * speed,
    left: n,
    stop: m === 'stop',
    ...(m === 'through' && { through: true, clear_x: s.x, clear_z: s.z }),
  };
  return leap
    ? { ...s, move, vy: (p.gravity * p.dt * (n + 1)) / 2, on_ground: false }
    : { ...s, move };
}

/** How far apart a blink tests its way. */
const BLINK_STEP = 0.25;

/** Moves s at once along w, as far as it can, as the server's Blink: tested every BLINK_STEP, it stops short of a solid, an uphill steep cell or the map's edge, passes through mobs, and ends at the farthest point tested that no mob's body holds. */
export function blink(s: State, w: Way, g: Geometry, p: Params): State {
  const walls: Geometry = { ...g, blockers: [] };
  let at = s;
  let end = s;
  for (let gone = 0; gone < w.length;) {
    let d = w.length - gone;
    if (d > BLINK_STEP) {
      d = BLINK_STEP;
    }
    let next = at;
    const dx = w.x * d;
    if (dx !== 0) {
      if (slide(next, dx, false, walls, p)[0] !== dx) {
        break;
      }
      next = { ...next, x: next.x + dx };
    }
    const dz = w.z * d;
    if (dz !== 0) {
      if (slide(next, dz, true, walls, p)[0] !== dz) {
        break;
      }
      next = { ...next, z: next.z + dz };
    }
    at = next;
    gone = gone + d;
    if (!inMob(at, g, p)) {
      end = at;
    }
  }
  return end;
}

/** Whether a mob's body holds s's. */
function inMob(s: State, g: Geometry, p: Params): boolean {
  for (const b of g.blockers ?? []) {
    if (s.y < b.top && s.y + p.height > b.bottom) {
      const dx = s.x - b.x;
      const dz = s.z - b.z;
      const r = b.radius + p.radius;
      if (dx * dx + dz * dz < r * r) {
        return true;
      }
    }
  }
  return false;
}

/** Advances s by one sub-step in the server's order. */
export function step(s: State, pressed: Input, g: Geometry, p: Params): State {
  const held = (s.held ?? 0) > 0; // held where it is, its heading kept, a skill's move ended
  const input = held ? NO_KEYS : pressed;
  let x = keys(input.right, input.left);
  let z = keys(input.up, input.down);
  if (x !== 0 && z !== 0) {
    x = x * DIAGONAL;
    z = z * DIAGONAL;
  }
  let { hx, hz, vy, on_ground: onGround } = s;
  if (x !== 0 || z !== 0) {
    hx = x;
    hz = z;
  }
  const moving = !held && (s.move?.left ?? 0) > 0;
  const walks = (x !== 0 || z !== 0) && !moving;
  const sprintAt = sprintSteps(p);
  const { sprint: walked = 0, ...uncounted } = s;
  const sprinting = walks && sprintAt > 0 && walked >= sprintAt;
  // standing, a skill's move, a hold or a fight ends the count; a jump keeps it
  const count = !walks || sprintAt === 0 ? 0 : walked < sprintAt ? walked + 1 : walked;
  let speed = sprinting ? p.walk_speed * p.sprint!.speed : p.walk_speed;
  if ((s.slow?.left ?? 0) > 0) {
    speed = speed * s.slow!.factor;
  }
  // a skill's move replaces the walk
  const vx = moving ? s.move!.vx : x * speed;
  const vz = moving ? s.move!.vz : z * speed;

  if (onGround && input.jump) {
    vy = p.jump_velocity;
    onGround = false;
  }

  vy -= p.gravity * p.dt;
  if (vy < -p.max_fall_speed) {
    vy = -p.max_fall_speed;
  }

  let at: State = { ...uncounted, vx, vy, vz, on_ground: onGround, hx, hz };
  let intoSteep = false; // moved into a cell steep along the way it moved
  let cut = true; // every key's move was cut short
  let cylinder = -1; // the cylinder that cut one
  let atMob = false; // whether a mob's body cut a move short
  // what the body slides against: no mobs for a move passing through them
  const solids: Geometry = moving && s.move!.through ? { ...g, blockers: [] } : g;
  const dx = at.vx * p.dt;
  const dz = at.vz * p.dt;
  if (dx !== 0) {
    const [moved, into, by, mob] = slide(at, dx, false, solids, p);
    at = { ...at, x: at.x + moved, vx: moved !== dx ? 0 : at.vx };
    intoSteep = into;
    atMob = mob;
    if (moved === dx) {
      cut = false;
    } else if (cylinder < 0) {
      cylinder = by;
    }
  }
  if (dz !== 0) {
    const [moved, into, by, mob] = slide(at, dz, true, solids, p);
    at = { ...at, z: at.z + moved, vz: moved !== dz ? 0 : at.vz };
    intoSteep = intoSteep || into;
    atMob = atMob || mob;
    if (moved === dz) {
      cut = false;
    } else if (cylinder < 0) {
      cylinder = by;
    }
  }
  if (cut && cylinder >= 0) {
    const [rounded, into] = round(at, g.cylinders[cylinder]!, dx, dz, solids, p);
    at = rounded;
    intoSteep = intoSteep || into;
  }

  const walking = at.on_ground;
  const from = at.y;
  let y = at.y + at.vy * p.dt;
  vy = at.vy;
  const under = ground(at.x, at.z, from + p.step, g);
  if (y <= under && vy <= 0) {
    y = under; // lands, or keeps walking up a slope
    vy = 0;
    onGround = true;
  } else if (y < under) {
    y = under; // rising past a lip: lifted onto it, still rising
    onGround = false;
  } else if (walking && !intoSteep && y - under <= p.snap) {
    y = under; // walks down a slope instead of hopping, but falls off a cliff
    vy = 0;
    onGround = true;
  } else {
    onGround = false;
  }
  const { move, held: heldFor, slow, ...body } = at;
  let next: State = { ...body, y, vy, on_ground: onGround, ...(count > 0 && { sprint: count }) };
  if (moving && move) {
    const inside = move.through === true && inMob(next, g, p);
    const kept = move.through && !inside ? { ...move, clear_x: next.x, clear_z: next.z } : move;
    const left = move.left - 1;
    if (left > 0 && !(move.stop && atMob)) {
      next = { ...next, move: { ...kept, left } };
    } else if (inside) {
      next = { ...next, x: move.clear_x!, z: move.clear_z! }; // run out inside a mob: back to the farthest point clear of them, as a blink ends
    }
  } else if (move && !held) {
    next = { ...next, move };
  }
  if (heldFor !== undefined && heldFor > 1) {
    next = { ...next, held: heldFor - 1 };
  }
  if (slow && slow.left > 1) {
    next = { ...next, slow: { ...slow, left: slow.left - 1 } };
  }
  return next;
}

/** 1 for the first key alone, -1 for the second alone, else 0. */
function keys(plus: boolean, minus: boolean): number {
  if (plus && !minus) {
    return 1;
  }
  if (minus && !plus) {
    return -1;
  }
  return 0;
}

/** s, stopped at the rim of c, moved round it by the share of (dx, dz) along its tangent, at its distance from c's centre; x and z each go as far as a step lets them, the one leaving c first. Also whether into a steep cell. */
function round(
  s: State,
  c: Cylinder,
  dx: number,
  dz: number,
  g: Geometry,
  p: Params,
): [State, boolean] {
  const nx = s.x - c.x;
  const nz = s.z - c.z;
  const r = Math.sqrt(nx * nx + nz * nz);
  if (r === 0) {
    return [s, false];
  }
  const tx = -nz / r;
  const tz = nx / r;
  const along = dx * tx + dz * tz;
  if (along === 0) {
    return [s, false]; // pressing straight at the centre
  }
  let ox = nx + along * tx;
  let oz = nz + along * tz;
  const k = r / Math.sqrt(ox * ox + oz * oz);
  ox = ox * k;
  oz = oz * k;
  const mx = c.x + ox - s.x;
  const mz = c.z + oz - s.z;
  const xFirst = Math.abs(ox) >= Math.abs(nx);
  let at = s;
  let steep = false;
  for (const alongZ of [!xFirst, xFirst]) {
    const d = alongZ ? mz : mx;
    if (d !== 0) {
      const [moved, into] = slide(at, d, alongZ, g, p);
      at = alongZ ? { ...at, z: at.z + moved } : { ...at, x: at.x + moved };
      steep = steep || into;
    }
  }
  return [at, steep];
}

/** How far of d a body moves along x, or z when alongZ, whether into a cell steep that way, the cylinder that cut it short, else -1, and whether a mob did: edges, solids and mobs stop it at contact, terrain it can't climb is a wall. */
function slide(
  s: State,
  d: number,
  alongZ: boolean,
  g: Geometry,
  p: Params,
): [number, boolean, number, boolean] {
  const at = alongZ ? s.z : s.x;
  const across = alongZ ? s.x : s.z;
  const size = alongZ ? g.size_z : g.size_x;
  d = limit(d, -Infinity, p.radius - at);
  d = limit(d, size - p.radius - at, Infinity);
  for (const b of g.boxes) {
    if (b.top > s.y + p.step) {
      const span = boxSpan(b, s.x, s.z, alongZ, p.radius);
      if (span) {
        d = limit(d, span[0], span[1]);
      }
    }
  }
  let by = -1;
  let mob = false;
  g.cylinders.forEach((c, i) => {
    if (c.top > s.y + p.step) {
      const span = circleSpan(c.x, c.z, c.radius + p.radius, s.x, s.z, alongZ);
      if (span) {
        const cut = limit(d, span[0], span[1]);
        if (cut !== d) {
          d = cut;
          by = i;
        }
      }
    }
  });
  for (const b of g.blockers ?? []) {
    if (s.y < b.top && s.y + p.height > b.bottom) {
      const span = circleSpan(b.x, b.z, b.radius + p.radius, s.x, s.z, alongZ);
      if (span) {
        const cut = limit(d, span[0], span[1]);
        if (cut !== d) {
          d = cut;
          by = -1;
          mob = true;
        }
      }
    }
  }
  if (d === 0) {
    return [0, false, by, mob];
  }
  const x = alongZ ? across : at + d;
  const z = alongZ ? at + d : across;
  const isSteep = steep(g.terrain, x, z, alongZ);
  const h = height(g.terrain, x, z);
  if (s.on_ground) {
    if (isSteep && h > s.y) {
      return [0, false, -1, mob]; // uphill into a steep cell
    }
  } else if (isSteep && h > height(g.terrain, s.x, s.z)) {
    if (s.y < top(g.terrain, x, z)) {
      return [0, false, -1, mob]; // in the air, uphill, a steep cell is a cliff as high as its highest sample
    }
  } else if (h > s.y) {
    return [0, false, -1, mob];
  }
  return [d, isSteep, by, mob];
}

/** Cuts a move of d against the open span (lo, hi) of moves that overlap: it stops at the span's edge, and from inside only goes no deeper. */
function limit(d: number, lo: number, hi: number): number {
  if (lo >= 0) {
    return d > lo ? lo : d;
  }
  if (hi <= 0) {
    return d < hi ? hi : d;
  }
  return depth(d, lo, hi) > depth(0, lo, hi) ? 0 : d;
}

/** How far a move of d lies inside the span (lo, hi), negative outside. */
function depth(d: number, lo: number, hi: number): number {
  return d - lo < hi - d ? d - lo : hi - d;
}

/** The open span of moves along x, or z when alongZ, from (x, z) that bring it within reach of (cx, cz). */
function circleSpan(
  cx: number,
  cz: number,
  reach: number,
  x: number,
  z: number,
  alongZ: boolean,
): [number, number] | null {
  const along = alongZ ? z - cz : x - cx;
  const across = alongZ ? x - cx : z - cz;
  const room = reach * reach - across * across;
  if (room <= 0) {
    return null;
  }
  const half = Math.sqrt(room);
  return [-along - half, -along + half];
}

/** The open span of moves along x, or z when alongZ, from (x, z) that bring a circle of radius r into b: in b's frame, two slabs and four corner discs. */
function boxSpan(
  b: Box,
  x: number,
  z: number,
  alongZ: boolean,
  r: number,
): [number, number] | null {
  const dx = x - b.x;
  const dz = z - b.z;
  const u = dx * b.ux + dz * b.uz;
  const v = dz * b.ux - dx * b.uz;
  const du = alongZ ? b.uz : b.ux;
  const dv = alongZ ? b.ux : -b.uz;
  let lo = Infinity;
  let hi = -Infinity;
  const add = (span: [number, number] | null) => {
    if (span) {
      if (span[0] < lo) {
        lo = span[0];
      }
      if (span[1] > hi) {
        hi = span[1];
      }
    }
  };
  add(rectSpan(u, v, du, dv, b.half_w + r, b.half_d));
  add(rectSpan(u, v, du, dv, b.half_w, b.half_d + r));
  for (const [cu, cv] of [
    [b.half_w, b.half_d],
    [-b.half_w, b.half_d],
    [-b.half_w, -b.half_d],
    [b.half_w, -b.half_d],
  ] as const) {
    add(discSpan(u - cu, v - cv, du, dv, r));
  }
  return lo < hi ? [lo, hi] : null;
}

/** The open span of s for which (u, v) + s(du, dv) lies inside |u| < hu, |v| < hv. */
function rectSpan(
  u: number,
  v: number,
  du: number,
  dv: number,
  hu: number,
  hv: number,
): [number, number] | null {
  let lo = -Infinity;
  let hi = Infinity;
  for (const [at, rate, half] of [
    [u, du, hu],
    [v, dv, hv],
  ] as const) {
    if (rate === 0) {
      if (Math.abs(at) >= half) {
        return null;
      }
      continue;
    }
    let a = (-half - at) / rate;
    let b = (half - at) / rate;
    if (a > b) {
      [a, b] = [b, a];
    }
    if (a > lo) {
      lo = a;
    }
    if (b < hi) {
      hi = b;
    }
  }
  return lo < hi ? [lo, hi] : null;
}

/** The open span of s for which (pu, pv) + s(du, dv) lies within r of the origin; a line passing at exactly r misses. */
function discSpan(
  pu: number,
  pv: number,
  du: number,
  dv: number,
  r: number,
): [number, number] | null {
  const a = du * du + dv * dv;
  const cross = pu * dv - pv * du;
  const room = r * r * a - cross * cross;
  if (room <= 0) {
    return null;
  }
  const mid = -(pu * du + pv * dv) / a;
  const half = Math.sqrt(room) / a;
  return [mid - half, mid + half];
}

/** The ground under (x, z): the terrain, or the top of a solid holding the point that is no higher than below. */
export function ground(x: number, z: number, below: number, g: Geometry): number {
  let top = height(g.terrain, x, z);
  for (const b of g.boxes) {
    if (b.top > top && b.top <= below) {
      const dx = x - b.x;
      const dz = z - b.z;
      const u = dx * b.ux + dz * b.uz;
      const v = dz * b.ux - dx * b.uz;
      if (Math.abs(u) <= b.half_w && Math.abs(v) <= b.half_d) {
        top = b.top;
      }
    }
  }
  for (const c of g.cylinders) {
    if (c.top > top && c.top <= below) {
      const dx = x - c.x;
      const dz = z - c.z;
      if (dx * dx + dz * dz <= c.radius * c.radius) {
        top = c.top;
      }
    }
  }
  return top;
}
