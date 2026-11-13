// Mirrors server/internal/sim op for op, so testdata/movement replays on both; (x, y) is the feet's centre, y down.
import { movement } from '@/gamedata/movement';

import type { Input } from './input';

export const NO_PLATFORM = -1;

export interface Params {
  readonly dt: number; // one sub-step, 1/60 s
  readonly walk_speed: number; // px/s
  readonly jump_velocity: number; // px/s, upward
  readonly gravity: number; // px/s²
  readonly max_fall_speed: number; // px/s
  readonly climb_speed: number; // px/s
  readonly drop_through: number; // seconds a dropped-through platform is ignored
  readonly width: number;
  readonly height: number;
  readonly casting_speed: number; // walking and climbing while casting, as a share of the speed
}

export const params: Params = movement;

/** 1 plus a move speed, rounded to thousandths so the server and the client multiply by the same number. */
export function speedFactor(moveSpeed: number): number {
  return Math.round((1 + moveSpeed) * 1000) / 1000;
}

/** p played f times as fast, as the server's Params.Faster: speeds by f and gravity by f², so a jump takes 1/f of the time and, stepped, peaks slightly lower; the casting speed stays a share. */
export function faster(p: Params, f: number): Params {
  if (f === 1) {
    return p;
  }
  return {
    ...p,
    walk_speed: p.walk_speed * f,
    climb_speed: p.climb_speed * f,
    jump_velocity: p.jump_velocity * f,
    max_fall_speed: p.max_fall_speed * f,
    gravity: p.gravity * (f * f),
  };
}

export interface Rect {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

export interface Geometry {
  readonly solids: readonly Rect[];
  readonly platforms: readonly Rect[]; // one-way: only the top edge counts, only when landing
  readonly ropes: readonly Rect[];
  readonly blockers?: readonly Rect[]; // mobs: a player's x overlap with one can't grow; the y move ignores them
}

export interface State {
  readonly x: number;
  readonly y: number;
  readonly vx: number;
  readonly vy: number;
  readonly on_ground: boolean;
  readonly climbing: boolean;
  readonly facing: 1 | -1;
  readonly ground: number; // platform index stood on, NO_PLATFORM on a solid or in the air
  readonly drop_platform: number; // platform ignored while drop_timer > 0
  readonly drop_timer: number;
  readonly casting: number; // sub-steps left of using a skill: walking and climbing at casting_speed, facing held
}

export function standing(x: number, y: number): State {
  return {
    x,
    y,
    vx: 0,
    vy: 0,
    on_ground: true,
    climbing: false,
    facing: 1,
    ground: NO_PLATFORM,
    drop_platform: NO_PLATFORM,
    drop_timer: 0,
    casting: 0,
  };
}

interface Working {
  x: number;
  y: number;
  vx: number;
  vy: number;
  on_ground: boolean;
  climbing: boolean;
  facing: 1 | -1;
  ground: number;
  drop_platform: number;
  drop_timer: number;
  casting: number;
}

/** One sub-step of p.dt. The phase order matches the server's sim.Step; do not reorder. */
export function step(state: State, input: Input, g: Geometry, p: Params): State {
  const s: Working = { ...state };

  if (s.casting === 0) {
    // casting holds the facing
    if (input.right) {
      s.facing = 1;
    } else if (input.left) {
      s.facing = -1;
    }
  }

  if (!s.climbing && (input.up || input.down)) {
    for (const rope of g.ropes) {
      if (canEnter(s, input, rope, p)) {
        s.climbing = true;
        s.x = rope.x + rope.w / 2;
        s.vx = 0;
        s.vy = 0;
        s.on_ground = false;
        s.ground = NO_PLATFORM;
        break;
      }
    }
  }

  if (s.climbing) {
    if (input.jump) {
      s.climbing = false;
      s.vy = -p.jump_velocity;
      s.vx = walkVelocity(s, input, p);
    } else {
      s.vx = 0;
      s.vy = 0;
      const climb = pace(s, p.climb_speed, p);
      if (input.up && !input.down) {
        s.y -= climb * p.dt;
      } else if (input.down && !input.up) {
        s.y += climb * p.dt;
      }
      if (!onAnyRope(s, g, p)) {
        s.climbing = false;
      }
      return tickTimer(s, p);
    }
  } else {
    s.vx = walkVelocity(s, input, p);

    if (s.on_ground && input.jump) {
      if (input.down && s.ground !== NO_PLATFORM) {
        s.drop_platform = s.ground;
        s.drop_timer = p.drop_through;
      } else {
        s.vy = -p.jump_velocity;
      }
      s.on_ground = false;
      s.ground = NO_PLATFORM;
    }
  }

  s.vy += p.gravity * p.dt;
  if (s.vy > p.max_fall_speed) {
    s.vy = p.max_fall_speed;
  }

  const from = box(s, p);
  const fromX = s.x;
  s.x += s.vx * p.dt;
  for (const solid of g.solids) {
    if (!overlaps(box(s, p), solid)) {
      continue;
    }
    if (s.vx > 0) {
      s.x = solid.x - p.width / 2;
    } else if (s.vx < 0) {
      s.x = solid.x + solid.w + p.width / 2;
    }
    s.vx = 0;
  }
  for (const b of g.blockers ?? []) {
    if (!overlaps(box(s, p), b)) {
      continue;
    }
    const x = held(from, fromX, s.x, b);
    if (x !== s.x) {
      s.x = x;
      s.vx = 0;
    }
  }

  const prevFeet = s.y;
  s.y += s.vy * p.dt;
  s.on_ground = false;
  s.ground = NO_PLATFORM;
  for (const solid of g.solids) {
    if (!overlaps(box(s, p), solid)) {
      continue;
    }
    if (s.vy > 0) {
      s.y = solid.y;
      s.on_ground = true;
    } else if (s.vy < 0) {
      s.y = solid.y + solid.h + p.height;
    }
    s.vy = 0;
  }
  if (s.vy > 0) {
    // A one-way platform catches only feet that were at or above its top before the move.
    for (let i = 0; i < g.platforms.length; i++) {
      const plat = g.platforms[i]!;
      if (i === s.drop_platform || prevFeet > plat.y || s.y < plat.y) {
        continue;
      }
      if (overlapsX(s, plat, p)) {
        s.y = plat.y;
        s.vy = 0;
        s.on_ground = true;
        s.ground = i;
        break;
      }
    }
  }

  return tickTimer(s, p);
}

function walkVelocity(s: Working, input: Input, p: Params): number {
  if (input.right && !input.left) {
    return pace(s, p.walk_speed, p);
  }
  if (input.left && !input.right) {
    return -pace(s, p.walk_speed, p);
  }
  return 0;
}

/** A walking or climbing speed, at casting_speed while casting. */
function pace(s: Working, speed: number, p: Params): number {
  return s.casting > 0 ? speed * p.casting_speed : speed;
}

/** Counts the drop-through and the casting down by one sub-step. */
function tickTimer(s: Working, p: Params): State {
  s.drop_timer -= p.dt;
  if (s.drop_timer <= 0) {
    s.drop_timer = 0;
    s.drop_platform = NO_PLATFORM;
  }
  if (s.casting > 0) {
    s.casting--;
  }
  return s;
}

function canEnter(s: Working, input: Input, rope: Rect, p: Params): boolean {
  if (!feetOn(s, rope, p)) {
    return false;
  }
  return (input.up && s.y > rope.y) || (input.down && s.y < rope.y + rope.h);
}

function onAnyRope(s: Working, g: Geometry, p: Params): boolean {
  return g.ropes.some((rope) => feetOn(s, rope, p));
}

function feetOn(s: Working, r: Rect, p: Params): boolean {
  return s.y >= r.y && s.y <= r.y + r.h && overlapsX(s, r, p);
}

function overlapsX(s: Working, r: Rect, p: Params): boolean {
  return s.x - p.width / 2 < r.x + r.w && s.x + p.width / 2 > r.x;
}

function box(s: Working, p: Params): Rect {
  return { x: s.x - p.width / 2, y: s.y - p.height, w: p.width, h: p.height };
}

/** Where a box from, at x fromX, moving to toX ends against b, so that its x overlap with b doesn't grow; the server's sim.Held. */
function held(from: Rect, fromX: number, toX: number, b: Rect): number {
  if (toX > fromX && from.x < b.x && from.x + from.w < b.x + b.w) {
    return Math.min(toX, Math.max(fromX, b.x - from.w / 2));
  }
  if (toX < fromX && from.x + from.w > b.x + b.w && from.x > b.x) {
    return Math.max(toX, Math.min(fromX, b.x + b.w + from.w / 2));
  }
  return toX;
}

/** Touching edges do not overlap. */
export function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;
}
