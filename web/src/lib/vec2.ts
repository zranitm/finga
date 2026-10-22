// Plain 2D vectors over {x, y} objects. Pure functions, no game knowledge.
export interface Vec2 {
  readonly x: number;
  readonly y: number;
}

export function add(a: Vec2, b: Vec2): Vec2 {
  return { x: a.x + b.x, y: a.y + b.y };
}

export function scale(v: Vec2, k: number): Vec2 {
  return { x: v.x * k, y: v.y * k };
}

export function length(v: Vec2): number {
  return Math.hypot(v.x, v.y);
}

export function distance(a: Vec2, b: Vec2): number {
  return Math.hypot(b.x - a.x, b.y - a.y);
}
