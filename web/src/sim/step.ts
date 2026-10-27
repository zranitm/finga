// The movement simulation (page 5), a mirror of the Go package
// server/internal/sim: the same order of operations, so the
// vectors in testdata/movement replay exactly here and there. Coordinates
// are pixels with y growing downward. A character is a width × height box
// whose position (x, y) is the centre of its feet: the box spans
// [x-width/2, x+width/2) by [y-height, y). Rectangles are half-open, so a
// box flush against an edge does not touch it.
import { movement } from '@/gamedata/movement';

import type { Input } from './input';

/** The ground and drop_platform value when no one-way platform is involved. */
export const NO_PLATFORM = -1;

export interface Params {
  readonly dt: number; // one sub-step, 1/60 s
  readonly walk_speed: number; // px/s
  readonly jump_velocity: number; // px/s, upward
  readonly gravity: number; // px/s²
  readonly max_fall_speed: number; // px/s
  readonly climb_speed: number; // px/s
  readonly drop_through: number; // seconds a dropped-through platform is ignored
  readonly width: number; // character box
  readonly height: number;
}

/** The constants from data/movement.yaml, through the generated gamedata. */
export const params: Params = movement;

export interface Rect {
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

/** What a map's object layers give the simulation; platform indexes refer into platforms. */
export interface Geometry {
  readonly solids: readonly Rect[]; // block movement on every side
  readonly platforms: readonly Rect[]; // one-way: only the top edge counts, only when landing
  readonly ropes: readonly Rect[]; // climbable while the feet are within them
}

/** A character's movement state; step returns a new one and never changes its argument. */
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
  readonly drop_timer: number; // seconds left of the drop-through
}

/** A character standing still at (x, y), facing right. */
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
  };
}

// A mutable copy for the duration of one step.
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
}

/**
 * step advances s by one sub-step of p.dt under the input, in this order:
 *
 * 1. Facing follows right or left (right wins when both are held).
 * 2. Rope entry: up with the feet inside a rope's span and rope above them,
 *    or down with rope below them, starts climbing, snapped to the rope's
 *    centre, with velocity cleared. The feet decide, not the box.
 * 3. Climbing: jump leaves the rope with a full jump (horizontal speed from
 *    left/right); otherwise the character moves at climb_speed up or down,
 *    and leaves the rope once the feet pass either end. No gravity and no
 *    collision while climbing; the step ends after the timer (9).
 * 4. Walking: vx is walk_speed to the held side, 0 when neither or both.
 * 5. Jump from the ground: with down while on a one-way platform it drops
 *    through (the platform is ignored for drop_through seconds); otherwise
 *    vy becomes -jump_velocity.
 * 6. Gravity: vy grows by gravity·dt, clamped at max_fall_speed.
 * 7. Move on x by vx·dt and push out of any solid, zeroing vx.
 * 8. Move on y by vy·dt. on_ground is cleared first. Moving down, a solid
 *    or a one-way platform whose top the feet were at or above before the
 *    move stops the fall: feet on its top, vy 0, on_ground set. Moving up, a
 *    solid stops the rise: head on its underside, vy 0. Solids are checked
 *    before platforms, each in map order; the ignored platform is skipped.
 * 9. The drop-through timer counts down and releases its platform at 0.
 */
export function step(state: State, input: Input, g: Geometry, p: Params = params): State {
  const s: Working = { ...state };

  // 1. Facing.
  if (input.right) {
    s.facing = 1;
  } else if (input.left) {
    s.facing = -1;
  }

  // 2. Rope entry.
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

  // 3. Climbing.
  if (s.climbing) {
    if (input.jump) {
      s.climbing = false;
      s.vy = -p.jump_velocity;
      s.vx = walkVelocity(input, p);
    } else {
      s.vx = 0;
      s.vy = 0;
      if (input.up && !input.down) {
        s.y -= p.climb_speed * p.dt;
      } else if (input.down && !input.up) {
        s.y += p.climb_speed * p.dt;
      }
      if (!onAnyRope(s, g, p)) {
        s.climbing = false; // past the top or the bottom: fall, or land on what is there
      }
      return tickTimer(s, p);
    }
  } else {
    // 4. Walking.
    s.vx = walkVelocity(input, p);

    // 5. Jump or drop through.
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

  // 6. Gravity.
  s.vy += p.gravity * p.dt;
  if (s.vy > p.max_fall_speed) {
    s.vy = p.max_fall_speed;
  }

  // 7. X.
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

  // 8. Y.
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

  // 9. Drop-through timer.
  return tickTimer(s, p);
}

function walkVelocity(input: Input, p: Params): number {
  if (input.right && !input.left) {
    return p.walk_speed;
  }
  if (input.left && !input.right) {
    return -p.walk_speed;
  }
  return 0;
}

function tickTimer(s: Working, p: Params): State {
  s.drop_timer -= p.dt;
  if (s.drop_timer <= 0) {
    s.drop_timer = 0;
    s.drop_platform = NO_PLATFORM;
  }
  return s;
}

// canEnter: the feet are within the rope's span and the box overlaps it on
// x, and there is rope left in the held direction (above the feet for up,
// below for down).
function canEnter(s: Working, input: Input, rope: Rect, p: Params): boolean {
  if (!feetOn(s, rope, p)) {
    return false;
  }
  return (input.up && s.y > rope.y) || (input.down && s.y < rope.y + rope.h);
}

function onAnyRope(s: Working, g: Geometry, p: Params): boolean {
  return g.ropes.some((rope) => feetOn(s, rope, p));
}

// feetOn: the feet are within r's vertical span (ends included) and the box
// overlaps r on x.
function feetOn(s: Working, r: Rect, p: Params): boolean {
  return s.y >= r.y && s.y <= r.y + r.h && overlapsX(s, r, p);
}

function overlapsX(s: Working, r: Rect, p: Params): boolean {
  return s.x - p.width / 2 < r.x + r.w && s.x + p.width / 2 > r.x;
}

function box(s: Working, p: Params): Rect {
  return { x: s.x - p.width / 2, y: s.y - p.height, w: p.width, h: p.height };
}

/** The half-open rectangle test: touching edges do not overlap. */
export function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;
}
