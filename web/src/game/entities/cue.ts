// The player's own cue that a press now cancels: a spark burst at the front of the sprite, which flashes white while it shows.
import type Phaser from 'phaser';

import { DEPTH } from '../depth';

export const CUE_MS = 140;
const BURST_MS = 200;
const RAYS = 6;

/** Draws the burst at (x, y), the sprite's front at chest height, growing and fading on its own. */
export function drawCue(scene: Phaser.Scene, x: number, y: number): void {
  const ring = scene.add.circle(x, y, 6).setStrokeStyle(3, 0xffffff).setDepth(DEPTH.effect);
  const rays = scene.add.graphics({ x, y }).setDepth(DEPTH.effect);
  rays.lineStyle(3, 0xfff2a8, 1);
  for (let k = 0; k < RAYS; k++) {
    const a = (k / RAYS) * Math.PI * 2;
    rays.lineBetween(Math.cos(a) * 5, Math.sin(a) * 5, Math.cos(a) * 11, Math.sin(a) * 11);
  }
  scene.tweens.add({
    targets: [ring, rays],
    scale: 1.8,
    alpha: 0,
    duration: BURST_MS,
    onComplete: () => {
      ring.destroy();
      rays.destroy();
    },
  });
}
