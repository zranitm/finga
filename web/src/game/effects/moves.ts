// A skill's move (skill-roster.md section 7): speed lines trailing a dash or leap along the body's last path, dust where a leap takes off, and a flash at both ends of a blink. Each reads the body as it's drawn, so the player's own predicted move and another's, drawn behind, show alike.
import type { Mesh } from '@babylonjs/core/Meshes/mesh';

import { movement } from '@/gamedata/movement';
import type { Move } from '@/progression/skills';

import { brighter } from './colors';
import { animate, ball, beatsOf, clamp01, Line } from './draw';
import { burst } from './particles';
import type { Drawn, Point, Stop } from './types';

/** How much of the body's path the lines show. */
export const TRAIL_MS = 160;
/** How long after the action's start its move may first show: from the next input, and another's drawn behind the newest frame. */
export const LATE_MS = 250;
/** How long a blink's flash lasts. */
export const FLASH_MS = 250;
/** Over this speed, twice a walk's, the body is moving as no walk does; the lines are whole a walk's speed past it. */
export const FAST = 2 * movement.walk_speed;
/** How far from a blink's start the body has to be for it to have landed. */
export const LANDED_M = 1;

const LINES = [
  { up: 0.35, side: -0.12 },
  { up: 0.8, side: 0.14 },
  { up: 1.2, side: -0.08 },
  { up: 1.55, side: 0.1 },
] as const;
const POINTS = 8;
const LINE_M = 0.025;
const LINE_ALPHA = 0.85;
const DUST = 0xa89070;
const DUST_UP = 0.2; // over the ground, so none starts in it
const TAKEOFF_M = 0.3; // how far from the start a leap has to be for its dust
const FLASH_W = 0.9;
const FLASH_H = 2.1;

interface Timed {
  readonly t: number;
  readonly p: Point;
}

/** n points along path, evenly by time from `from` to `to`, each held at the path's ends. */
function sampled(path: readonly Timed[], from: number, to: number, n: number): Point[] {
  const out: Point[] = [];
  let j = 0;
  for (let k = 0; k < n; k++) {
    const t = from + ((to - from) * k) / (n - 1);
    while (j < path.length - 2 && path[j + 1]!.t <= t) {
      j++;
    }
    const a = path[j]!;
    const b = path[j + 1] ?? a;
    const f = b.t > a.t ? clamp01((t - a.t) / (b.t - a.t)) : 1;
    out.push({
      x: a.p.x + (b.p.x - a.p.x) * f,
      y: a.p.y + (b.p.y - a.p.y) * f,
      z: a.p.z + (b.p.z - a.p.z) * f,
    });
  }
  return out;
}

/** A dash's or leap's speed lines, in the skill's colour: thin streaks at the body's knees to its head, trailing it over its last TRAIL_MS while it moves faster than a walk can; a leap kicks up dust where it takes off. */
export function trail(color: number, move: Move): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const until = b.start + move.time_ms + LATE_MS + TRAIL_MS;
    const lines = LINES.map(() => new Line(fx, brighter(color, 0.4), POINTS, LINE_M));
    const path: Timed[] = [];
    let dust: Stop | null = null;
    const parts = [...lines, { dispose: () => dust?.() }];
    return animate(fx, now, until, parts, (t) => {
      const body = o.follow?.() ?? o;
      path.push({ t, p: { x: body.x, y: body.y, z: body.z } });
      while (path.length > 2 && path[1]!.t <= t - TRAIL_MS) {
        path.shift();
      }
      const first = path[0]!;
      const dx = body.x - first.p.x;
      const dz = body.z - first.p.z;
      const across = Math.hypot(dx, dz);
      const speed = t > first.t ? (across * 1000) / (t - first.t) : 0;
      const alpha = LINE_ALPHA * clamp01((speed - FAST) / movement.walk_speed);
      if (move.kind === 'leap' && !dust && Math.hypot(body.x - o.x, body.z - o.z) > TAKEOFF_M) {
        dust = burst(
          fx,
          { x: o.x, y: o.y + DUST_UP, z: o.z },
          {
            count: 18,
            color: DUST,
            size: [0.35, 0.6],
            speed: [0.8, 1.8],
            life: [0.35, 0.55],
            spread: 0.3,
            rise: 0.6,
            blend: 'over',
          },
        );
      }
      if (alpha === 0) {
        lines.forEach((l) => l.draw([], 0));
        return;
      }
      const sx = dz / across; // to the path's right
      const sz = -dx / across;
      const points = sampled(path, t - TRAIL_MS, t, POINTS);
      lines.forEach((l, i) => {
        const { up, side } = LINES[i]!;
        l.draw(
          points.map((p) => ({ x: p.x + sx * side, y: p.y + up, z: p.z + sz * side })),
          alpha,
        );
      });
    });
  };
}

/** A blink's flash, in the skill's colour: a tall glow and sparkles where the caster stood at its start, and again where the body lands once it's drawn away from there. */
export function blinkFlash(color: number): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const glow = brighter(color, 0.5);
    const start: Point = { x: o.x, y: o.y, z: o.z };
    const flashes: { mesh: Mesh; from: number; sparkles: Stop }[] = [];
    const flash = (at: Point, t: number) => {
      const mesh = ball(fx, glow, 1, 'add');
      mesh.position.set(at.x, at.y + FLASH_H / 2, at.z);
      const sparkles = burst(
        fx,
        { x: at.x, y: at.y + 1, z: at.z },
        {
          count: 16,
          color: glow,
          size: [0.12, 0.25],
          speed: [1, 2.5],
          life: [0.2, 0.35],
          spread: 0.4,
          blend: 'add',
        },
      );
      flashes.push({ mesh, from: t, sparkles });
    };
    const parts = [
      {
        dispose: () =>
          flashes.forEach((f) => {
            f.mesh.dispose();
            f.sparkles();
          }),
      },
    ];
    return animate(fx, now, b.start + LATE_MS + FLASH_MS, parts, (t) => {
      if (flashes.length === 0 && t >= b.start) {
        flash(start, t);
      }
      const body = o.follow?.() ?? o;
      if (flashes.length === 1 && Math.hypot(body.x - start.x, body.z - start.z) > LANDED_M) {
        flash(body, t);
      }
      for (const f of flashes) {
        const k = clamp01((t - f.from) / FLASH_MS);
        f.mesh.scaling.set(
          FLASH_W * (1 - 0.7 * k),
          FLASH_H * (1 - 0.2 * k),
          FLASH_W * (1 - 0.7 * k),
        );
        f.mesh.visibility = 0.9 * (1 - k) * (1 - k);
        f.mesh.setEnabled(k < 1);
      }
    });
  };
}
