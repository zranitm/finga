// The drawn hits (combat-effects.md section 4): a spark at the target's middle sized to it, physical or magic, a crit's burst on top, and a miss's grey puff.
import { burst } from './particles';
import type { Drawn, PlayOptions } from './types';

const DEFAULT_SIZE = { radius: 0.5, height: 1 };

/** A spark's reach in metres: about half the target's larger side, so a boss's is bigger and a bat's still shows. */
function reach(o: PlayOptions): number {
  const s = o.size ?? DEFAULT_SIZE;
  return Math.min(1.1, Math.max(0.3, 0.45 * Math.max(2 * s.radius, s.height)));
}

const physical: Drawn = (fx, o) => {
  const r = reach(o);
  return burst(fx, o, {
    count: 16,
    color: 0xfff0c8,
    fade: 0xff9a40,
    size: [0.08 * r, 0.16 * r],
    speed: [6 * r, 10 * r],
    life: [0.1, 0.16],
    blend: 'add',
  });
};

const magic: Drawn = (fx, o) => {
  const r = reach(o);
  return burst(fx, o, {
    count: 14,
    color: 0xc890ff,
    fade: 0xf0e0ff,
    size: [0.12 * r, 0.22 * r],
    speed: [3 * r, 5 * r],
    life: [0.16, 0.22],
    spread: 0.2 * r,
    blend: 'add',
  });
};

const crit: Drawn = (fx, o) => {
  const r = reach(o) * 1.4;
  return burst(fx, o, {
    count: 24,
    color: 0xffd166,
    fade: 0xfffbe8,
    size: [0.12 * r, 0.24 * r],
    speed: [7 * r, 12 * r],
    life: [0.16, 0.24],
    blend: 'add',
  });
};

/** A few grey puffs that rise and thin: the blow went past. */
const miss: Drawn = (fx, o) => {
  const r = reach(o);
  return burst(fx, o, {
    count: 5,
    color: 0xb8b8c8,
    size: [0.25 * r, 0.4 * r],
    speed: [0.3, 0.6],
    life: [0.2, 0.24],
    spread: 0.25 * r,
    rise: 2,
    blend: 'over',
  });
};

export const hitsDrawn = {
  'hit.physical': physical,
  'hit.magic': magic,
  'hit.crit': crit,
  'hit.miss': miss,
} as const;
