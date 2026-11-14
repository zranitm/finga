// A cancel's afterimage: the pose the cancel cut off, left behind in white with a dark rim, fading as it drifts back.
import type Phaser from 'phaser';

import { DEPTH } from '../depth';

import { EDGE, UNSEEN_ALPHA, everyFrame, warmOnce } from './draw';
import type { Drawn } from './types';

const FILL = 1; // Phaser's fill tint mode: the sprite's shape in one colour
const FADE_MS = 300;
const FROM_ALPHA = 0.8;
const DRIFT_PX = 14; // most of it shows from behind the new pose
const RIM_PX = 3; // how far the dark rim reaches, so the ghost reads on pale skies and dark caves alike
const RIM_STRENGTH = 4; // solid for a pixel or two, then gone: a rim, not a halo
const RIM_QUALITY = 8;

/** A white copy of s with the dark rim, under the player, and what its fade goes through. */
function ghostOf(scene: Phaser.Scene, s: Phaser.GameObjects.Sprite) {
  const ghost = scene.add
    .sprite(s.x, s.y, s.texture.key, s.frame.name)
    .setOrigin(s.originX, s.originY)
    .setScale(s.scaleX, s.scaleY)
    .setAngle(s.angle)
    .setFlipX(s.flipX)
    .setTint(0xffffff)
    .setTintMode(FILL)
    .setDepth(DEPTH.afterimage)
    .enableFilters();
  ghost.filters?.external
    .addGlow(EDGE, RIM_STRENGTH, 0, 1, false, RIM_QUALITY, RIM_PX)
    .setPaddingOverride(null); // WebGL only; padded to its reach, or the frame's edge cuts it
  const fading = ghost.filterCamera ?? ghost; // the filter's camera fades rim and fill as one; fading the sprite would leave the rim solid
  return { ghost, fading };
}

export const afterimage: Drawn = (scene, o, now) => {
  const s = o.copy;
  if (!s) {
    return () => undefined;
  }
  const { ghost, fading } = ghostOf(scene, s);
  const x = s.x;
  return everyFrame(
    scene,
    now + FADE_MS,
    (t) => {
      const k = (t - now) / FADE_MS;
      fading.setAlpha(FROM_ALPHA * (1 - k));
      ghost.setX(x - o.facing * DRIFT_PX * k);
    },
    () => ghost.destroy(),
  );
};

/** A ghost of s drawn once, unseen, where a cancel's is: the first cancel then finds its shader and passes built, and doesn't hitch. */
export function warmAfterimage(scene: Phaser.Scene, s: Phaser.GameObjects.Sprite): void {
  const { ghost, fading } = ghostOf(scene, s);
  fading.setAlpha(UNSEEN_ALPHA);
  warmOnce(scene, ghost);
}
