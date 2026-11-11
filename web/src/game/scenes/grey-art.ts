// A tint multiplies onto the art, so a tinted mob or class draws from a grey copy brightened to white: the green slime tinted blue would stay green.
import type Phaser from 'phaser';

import { mobSprites } from '../entities/mob';
import { playerSprites } from '../entities/player';
import { animations, greyKey } from '../keys';
import type { Anim } from '../keys';

/** RGBA pixels made grey by luminance, then scaled so the lightest opaque one is white. */
export function greyPixels(p: Uint8ClampedArray): void {
  let top = 0;
  for (let i = 0; i < p.length; i += 4) {
    p[i] = 0.299 * p[i]! + 0.587 * p[i + 1]! + 0.114 * p[i + 2]!;
    if (p[i + 3]! > 0) {
      top = Math.max(top, p[i]!);
    }
  }
  const k = top > 0 ? 255 / top : 1;
  for (let i = 0; i < p.length; i += 4) {
    p[i] = p[i + 1] = p[i + 2] = p[i]! * k;
  }
}

/** Adds each texture's grey copy under greyKey. */
export function addGreyCopies(scene: Phaser.Scene, keys: readonly string[]): void {
  for (const key of keys) {
    if (scene.textures.exists(greyKey(key))) {
      continue;
    }
    const img = scene.textures.get(key).getSourceImage() as CanvasImageSource & {
      width: number;
      height: number;
    };
    const canvas = scene.textures.createCanvas(greyKey(key), img.width, img.height);
    if (!canvas) {
      continue;
    }
    const ctx = canvas.getContext();
    ctx.drawImage(img, 0, 0);
    const data = ctx.getImageData(0, 0, img.width, img.height);
    greyPixels(data.data);
    ctx.putImageData(data, 0, 0);
    canvas.refresh();
  }
}

/** What the tinted mobs and classes draw from: a grey copy of each of their textures, and of each of their animations over the grey frames. */
export function greyArt(): { textures: string[]; animations: Anim[] } {
  const loops = new Set([
    ...mobSprites.map((s) => s.move),
    ...playerSprites.flatMap((s) => [s.walk, s.climbAnim]),
  ]);
  const anims = animations.filter((a) => loops.has(a.key));
  const textures = new Set([
    ...mobSprites.flatMap((s) => [s.stand, s.dead]),
    ...playerSprites.flatMap((s) => [s.stand, s.jump, s.climb, s.hit]),
    ...anims.flatMap((a) => a.frames),
  ]);
  return {
    textures: [...textures],
    animations: anims.map((a) => ({ ...a, key: greyKey(a.key), frames: a.frames.map(greyKey) })),
  };
}
