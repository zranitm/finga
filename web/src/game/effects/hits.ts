// The drawn hits: a spark at the target's middle sized to it, physical or magic, a crit's burst on top, and a miss's puff.
import { EDGE, animate } from './draw';
import type { G } from './draw';
import type { Drawn, PlayOptions } from './types';

const DEFAULT_SIZE = { w: 32, h: 32 };

/** A spark's reach: about half the target's larger side, so a boss's is bigger and a bat's still shows. */
function reach(o: PlayOptions): number {
  const s = o.size ?? DEFAULT_SIZE;
  return Math.min(36, Math.max(10, 0.45 * Math.max(s.w, s.h)));
}

/** A burst over ms: it springs from half to full size at once and holds bright, then thins out. */
const burst =
  (
    ms: number,
    draw: (g: G, o: PlayOptions, size: number, alpha: number, k: number) => void,
  ): Drawn =>
  (scene, o, now) =>
    animate(scene, now + ms, (g, t) => {
      const k = (t - now) / ms;
      const size = 0.5 + 0.6 * (1 - (1 - k) ** 3);
      draw(g, o, size, k < 0.4 ? 1 : 1 - (k - 0.4) / 0.6, k);
    });

/** Rays out from the target's middle, long and short in turn, a long one along the facing. */
function rays(
  g: G,
  o: PlayOptions,
  n: number,
  r: number,
  width: number,
  color: number,
  alpha: number,
): void {
  for (let i = 0; i < n; i++) {
    const a = (i / n) * 2 * Math.PI;
    const len = i % 2 === 0 ? r : r * 0.55;
    const dx = o.facing * Math.cos(a);
    const dy = Math.sin(a);
    const [x0, y0, x1, y1] = [
      o.x + dx * r * 0.3,
      o.y + dy * r * 0.3,
      o.x + dx * len,
      o.y + dy * len,
    ];
    g.lineStyle(width + 3, EDGE, alpha * 0.45).lineBetween(x0, y0, x1, y1);
    g.lineStyle(width, color, alpha).lineBetween(x0, y0, x1, y1);
  }
}

const physical = burst(160, (g, o, size, a) => rays(g, o, 8, reach(o) * size, 3.5, 0xfff0c8, a));

const magic = burst(200, (g, o, size, a) => {
  const r = reach(o) * size;
  g.lineStyle(6, EDGE, a * 0.4).strokeCircle(o.x, o.y, r * 0.7);
  g.lineStyle(3, 0xc890ff, a).strokeCircle(o.x, o.y, r * 0.7);
  for (let i = 0; i < 4; i++) {
    const t = (i / 4) * 2 * Math.PI + Math.PI / 4;
    g.fillStyle(0xf0e0ff, a).fillCircle(o.x + Math.cos(t) * r, o.y + Math.sin(t) * r, 2.5);
  }
});

const crit = burst(240, (g, o, size, a) => {
  const r = reach(o) * 1.4 * size;
  rays(g, o, 12, r, 4, 0xffd166, a);
  g.fillStyle(0xfffbe8, a * 0.9).fillCircle(o.x, o.y, r * 0.22);
});

/** A grey puff that rises and thins: the blow went past. */
const miss = burst(220, (g, o, size, a, k) => {
  for (const [dx, dy, r] of [
    [-6, 2, 5],
    [5, 0, 6],
    [0, -5, 4],
  ] as const) {
    g.fillStyle(0xb8b8c8, a * 0.7).fillCircle(o.x + dx * size, o.y + dy - 10 * k, r * size);
  }
});

export const hitsDrawn = {
  'hit.physical': physical,
  'hit.magic': magic,
  'hit.crit': crit,
  'hit.miss': miss,
} as const;
