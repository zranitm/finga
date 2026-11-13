// A cancel's afterimage: the pose the cancel cut off, left behind in white, fading as it drifts back.
import { DEPTH } from '../depth';

import { everyFrame } from './draw';
import type { Drawn } from './types';

const FILL = 1; // Phaser's fill tint mode: the sprite's shape in one colour
const FADE_MS = 200;
const FROM_ALPHA = 0.5;
const DRIFT_PX = 6;

export const afterimage: Drawn = (scene, o, now) => {
  const s = o.copy;
  if (!s) {
    return () => undefined;
  }
  const ghost = scene.add
    .sprite(s.x, s.y, s.texture.key, s.frame.name)
    .setOrigin(s.originX, s.originY)
    .setScale(s.scaleX, s.scaleY)
    .setAngle(s.angle)
    .setFlipX(s.flipX)
    .setTint(0xffffff)
    .setTintMode(FILL)
    .setDepth(DEPTH.afterimage);
  const x = s.x;
  return everyFrame(
    scene,
    now + FADE_MS,
    (t) => {
      const k = (t - now) / FADE_MS;
      ghost.setAlpha(FROM_ALPHA * (1 - k)).setX(x - o.facing * DRIFT_PX * k);
    },
    () => ghost.destroy(),
  );
};
