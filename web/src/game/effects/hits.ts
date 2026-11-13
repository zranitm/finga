// The drawn hits: a spark at the target's middle sized to it, physical or magic, a crit's burst on top, and a miss's puff.
import type Phaser from 'phaser';

import { DEPTH } from '../depth';

import type { Drawn, PlayOptions, Stop } from './types';

const EDGE = 0x1d1a16;
const DEFAULT_SIZE = { w: 32, h: 32 };

/** A spark's reach: about half the target's larger side, so a boss's is bigger and a bat's still shows. */
function reach(o: PlayOptions): number {
  const s = o.size ?? DEFAULT_SIZE;
  return Math.min(36, Math.max(10, 0.45 * Math.max(s.w, s.h)));
}

/** Plays g from small and bright to full and gone over ms, then takes it away; stop takes it away sooner. */
function burst(
  scene: Phaser.Scene,
  g: Phaser.GameObjects.Graphics,
  ms: number,
  grow: number,
): Stop {
  g.setDepth(DEPTH.effect).setScale(0.5);
  const remove = () => {
    scene.tweens.killTweensOf(g);
    g.destroy();
  };
  scene.tweens.add({
    targets: g,
    scale: grow,
    alpha: 0,
    duration: ms,
    ease: 'Quad.easeOut',
    onComplete: remove,
  });
  return remove;
}

/** Rays out from the middle, long and short in turn, a long one along the facing. */
function rays(
  g: Phaser.GameObjects.Graphics,
  o: PlayOptions,
  n: number,
  r: number,
  width: number,
  color: number,
): void {
  for (let i = 0; i < n; i++) {
    const a = (i / n) * 2 * Math.PI;
    const len = i % 2 === 0 ? r : r * 0.55;
    const dx = o.facing * Math.cos(a);
    const dy = Math.sin(a);
    g.lineStyle(width + 3, EDGE, 0.45).lineBetween(dx * r * 0.3, dy * r * 0.3, dx * len, dy * len);
    g.lineStyle(width, color, 1).lineBetween(dx * r * 0.3, dy * r * 0.3, dx * len, dy * len);
  }
}

const physical: Drawn = (scene, o) => {
  const g = scene.add.graphics().setPosition(o.x, o.y);
  rays(g, o, 8, reach(o), 3, 0xfff0c8);
  return burst(scene, g, 160, 1.1);
};

const magic: Drawn = (scene, o) => {
  const r = reach(o);
  const g = scene.add.graphics().setPosition(o.x, o.y);
  g.lineStyle(6, EDGE, 0.4).strokeCircle(0, 0, r * 0.7);
  g.lineStyle(3, 0xc890ff, 1).strokeCircle(0, 0, r * 0.7);
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * 2 * Math.PI + Math.PI / 4;
    g.fillStyle(0xf0e0ff, 1).fillCircle(Math.cos(a) * r, Math.sin(a) * r, 2.5);
  }
  return burst(scene, g, 200, 1.2);
};

const crit: Drawn = (scene, o) => {
  const r = reach(o) * 1.4;
  const g = scene.add.graphics().setPosition(o.x, o.y);
  rays(g, o, 12, r, 4, 0xffd166);
  g.fillStyle(0xfffbe8, 0.9).fillCircle(0, 0, r * 0.22);
  return burst(scene, g, 240, 1.3);
};

/** A grey puff that rises and thins: the blow went past. */
const miss: Drawn = (scene, o) => {
  const g = scene.add.graphics().setPosition(o.x, o.y);
  for (const [dx, dy, r] of [
    [-6, 2, 5],
    [5, 0, 6],
    [0, -5, 4],
  ] as const) {
    g.fillStyle(0xb8b8c8, 0.7).fillCircle(dx, dy, r);
  }
  const stop = burst(scene, g, 220, 1.4);
  scene.tweens.add({ targets: g, y: o.y - 10, duration: 220 });
  return stop;
};

export const hitsDrawn = {
  'hit.physical': physical,
  'hit.magic': magic,
  'hit.crit': crit,
  'hit.miss': miss,
} as const;
