// What the drawn looks are made of: one Graphics redrawn each frame as a function of the scene's clock, like the pose, and the strokes on it.
import type Phaser from 'phaser';

import { movement } from '@/gamedata/movement';

import { DEPTH } from '../depth';
import type { Beats } from '../entities/beats';

import type { Body, PlayOptions, Stop } from './types';

export type G = Phaser.GameObjects.Graphics;
export interface Point {
  readonly x: number;
  readonly y: number;
}

export const EDGE = 0x1d1a16; // a dark edge under each stroke, so a pale one shows on pale ground too
export const UNSEEN_ALPHA = 0.01; // too faint to see, yet drawn: Phaser skips only alpha 0
export const BODY_MID = movement.height / 2;
const SHOT_PX_PER_MS = 1.4; // a shot crosses 420 px in 300 ms
const UNTIMED_STRIKE_MS = 150; // the beats of a play given none: a skill's strike and ready point
const UNTIMED_READY_MS = 450;

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** Runs step(now) each frame on the scene's clock until `until`, then end(); stop ends it sooner, as does the scene's shutdown. */
export function everyFrame(
  scene: Phaser.Scene,
  until: number,
  step: (now: number) => void,
  end: () => void,
): Stop {
  let live = true;
  const stop = () => {
    if (live) {
      live = false;
      scene.events.off('update', frame);
      scene.events.off('shutdown', stop); // a map change ends what plays
      end();
    }
  };
  function frame(): void {
    const now = scene.time.now;
    if (now >= until) {
      stop();
      return;
    }
    step(now);
  }
  scene.events.on('update', frame);
  scene.events.once('shutdown', stop);
  frame();
  return stop;
}

/** Keeps a filtered sprite for the scene's next render only: WebGL builds a filter's shader and passes on its first draw, so this builds them now. */
export function warmOnce(scene: Phaser.Scene, s: Phaser.GameObjects.Sprite): void {
  if (!s.filterCamera) {
    s.destroy(); // no WebGL: no filters, nothing to build
    return;
  }
  const done = () => {
    scene.events.off('render', done);
    scene.events.off('shutdown', done);
    s.destroy();
  };
  scene.events.once('render', done);
  scene.events.once('shutdown', done);
}

/** Redraws one Graphics each frame with draw(g, now) until `until`, then takes it away. */
export function animate(
  scene: Phaser.Scene,
  until: number,
  draw: (g: G, now: number) => void,
): Stop {
  const g = scene.add.graphics().setDepth(DEPTH.effect);
  return everyFrame(
    scene,
    until,
    (now) => {
      g.clear();
      draw(g, now);
    },
    () => g.destroy(),
  );
}

/** The play's beats, or for one given none, a skill's from now. */
export function beatsOf(o: PlayOptions, now: number): Beats {
  return (
    o.beats ?? {
      start: now,
      hit: now,
      strikeEnd: now + UNTIMED_STRIKE_MS,
      readyAt: now + UNTIMED_READY_MS,
      end: now + UNTIMED_READY_MS,
      roots: false,
    }
  );
}

/** How far through the strike now is: 0 at the hit, 1 at its end. */
export function through(b: Beats, now: number): number {
  return clamp01((now - b.hit) / Math.max(1, b.strikeEnd - b.hit));
}

/** How strongly a look shows: fully through the strike, then fading to nothing by `by`, the ready point unless sooner. */
export function strength(b: Beats, now: number, by = b.readyAt): number {
  if (now < b.hit) {
    return 0;
  }
  if (now < b.strikeEnd) {
    return 1;
  }
  return clamp01(1 - (now - b.strikeEnd) / Math.max(1, by - b.strikeEnd));
}

/** Where a shot from x along the facing is at now, flying from the hit at a shot's pace for `range` px; null before and after. */
export function flown(b: Beats, now: number, range: number): number | null {
  const d = (now - b.hit) * SHOT_PX_PER_MS;
  return d < 0 || d > range ? null : d;
}

/** How long a shot takes over range. */
export function flightMs(range: number): number {
  return range / SHOT_PX_PER_MS;
}

/** A shot drawn from x at its action's hit, flying at a shot's pace up to range px. */
export interface Shot {
  readonly x: number;
  readonly range: number;
  readonly beats: Beats;
}

/** When shot s reaches a target whose middle is at x, as Fire Arrow bursts on its foe: its head there, no further than its range. */
export function arrival(s: Shot, x: number): number {
  return s.beats.hit + flightMs(Math.min(s.range, Math.abs(x - s.x)));
}

/** A stroke through points over a dark edge; width may change along it, from the first point (0) to the last (1). */
export function stroke(
  g: G,
  points: readonly Point[],
  width: number | ((share: number) => number),
  color: number,
  alpha: number,
): void {
  const w = typeof width === 'number' ? () => width : width;
  for (const edge of [true, false]) {
    for (let i = 1; i < points.length; i++) {
      const a = points[i - 1]!;
      const b = points[i]!;
      const share = i / (points.length - 1);
      g.lineStyle(edge ? w(share) + 4 : w(share), edge ? EDGE : color, edge ? alpha * 0.5 : alpha);
      g.lineBetween(a.x, a.y, b.x, b.y);
    }
  }
}

/** Points on an ellipse arc about c toward facing: degrees from straight ahead, below positive, from a0 to a1. */
export function arcPoints(
  c: Point,
  rx: number,
  ry: number,
  facing: 1 | -1,
  a0: number,
  a1: number,
  steps = 12,
): Point[] {
  const out: Point[] = [];
  for (let i = 0; i <= steps; i++) {
    const a = ((a0 + ((a1 - a0) * i) / steps) * Math.PI) / 180;
    out.push({ x: c.x + facing * rx * Math.cos(a), y: c.y + ry * Math.sin(a) });
  }
  return out;
}

/** A fixed pseudo-random share in [0, 1) for n: the same jag each frame of one flicker. */
export function jitter(n: number): number {
  const s = Math.sin(n * 12.9898) * 43758.5453;
  return s - Math.floor(s);
}

/** A jagged line from a to b: `kinks` turns of up to amp px across it, the jags drawn from seed. */
export function zigzag(a: Point, b: Point, kinks: number, amp: number, seed: number): Point[] {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len = Math.hypot(dx, dy) || 1;
  const nx = -dy / len;
  const ny = dx / len;
  const out: Point[] = [a];
  for (let i = 1; i <= kinks; i++) {
    const t = i / (kinks + 1);
    const off = (jitter(seed * 31 + i) * 2 - 1) * amp;
    out.push({ x: a.x + dx * t + nx * off, y: a.y + dy * t + ny * off });
  }
  out.push(b);
  return out;
}

/** A body's middle. */
export function middleOf(b: Body): Point {
  return { x: b.x, y: b.y - b.h / 2 };
}

/** Mixes color toward white by share. */
export function brighter(color: number, share: number): number {
  const ch = (shift: number) => {
    const c = (color >> shift) & 0xff;
    return Math.round(c + (0xff - c) * share) << shift;
  };
  return ch(16) | ch(8) | ch(0);
}
