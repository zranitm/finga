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
}

/** Advances s by one sub-step in the server's order. */
export function step(s: State, input: Input, g: Geometry, p: Params): State {
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
  const vx = x * p.walk_speed;
  const vz = z * p.walk_speed;

  if (onGround && input.jump) {
    vy = p.jump_velocity;
    onGround = false;
  }

  vy -= p.gravity * p.dt;
  if (vy < -p.max_fall_speed) {
    vy = -p.max_fall_speed;
  }

  let at = { ...s, vx, vy, vz, on_ground: onGround, hx, hz };
  let intoSteep = false; // moved into a cell steep along the way it moved
  const dx = at.vx * p.dt;
  if (dx !== 0) {
    const [moved, into] = slide(at, dx, false, g, p);
    at = { ...at, x: at.x + moved, vx: moved !== dx ? 0 : at.vx };
    intoSteep = into;
  }
  const dz = at.vz * p.dt;
  if (dz !== 0) {
    const [moved, into] = slide(at, dz, true, g, p);
    at = { ...at, z: at.z + moved, vz: moved !== dz ? 0 : at.vz };
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
  return { ...at, y, vy, on_ground: onGround };
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

/** How far of d a body moves along x, or z when alongZ, and whether into a cell steep that way: edges, solids and mobs stop it at contact, terrain it can't climb is a wall. */
function slide(s: State, d: number, alongZ: boolean, g: Geometry, p: Params): [number, boolean] {
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
  for (const c of g.cylinders) {
    if (c.top > s.y + p.step) {
      const span = circleSpan(c.x, c.z, c.radius + p.radius, s.x, s.z, alongZ);
      if (span) {
        d = limit(d, span[0], span[1]);
      }
    }
  }
  for (const b of g.blockers ?? []) {
    if (s.y < b.top && s.y + p.height > b.bottom) {
      const span = circleSpan(b.x, b.z, b.radius + p.radius, s.x, s.z, alongZ);
      if (span) {
        d = limit(d, span[0], span[1]);
      }
    }
  }
  if (d === 0) {
    return [0, false];
  }
  const x = alongZ ? across : at + d;
  const z = alongZ ? at + d : across;
  const isSteep = steep(g.terrain, x, z, alongZ);
  const h = height(g.terrain, x, z);
  if (s.on_ground) {
    if (isSteep && h > s.y) {
      return [0, false]; // uphill into a steep cell
    }
  } else if (isSteep && h > height(g.terrain, s.x, s.z)) {
    if (s.y < top(g.terrain, x, z)) {
      return [0, false]; // in the air, uphill, a steep cell is a cliff as high as its highest sample
    }
  } else if (h > s.y) {
    return [0, false];
  }
  return [d, isSteep];
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
