// Loads in the background while the player logs in, then marks the assets ready for World.
import Phaser from 'phaser';

import { images, mapFile } from '../assets';
import { mobSprites } from '../entities/mob';
import { ANIM, MAP_IDS, SCENES, TEX, greyKey, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';

import { addGreyCopies } from './grey-art';
import { drawPlaceholders } from './placeholders';

interface Anim {
  readonly key: string;
  readonly frames: readonly string[];
  readonly frameRate: number;
}

export const animations: readonly Anim[] = [
  { key: ANIM.warriorWalk, frames: [TEX.warriorWalk1, TEX.warriorWalk2], frameRate: 8 },
  { key: ANIM.warriorClimb, frames: [TEX.warriorClimb1, TEX.warriorClimb2], frameRate: 6 },
  { key: ANIM.bowmanWalk, frames: [TEX.bowmanWalk1, TEX.bowmanWalk2], frameRate: 8 },
  { key: ANIM.bowmanClimb, frames: [TEX.bowmanClimb1, TEX.bowmanClimb2], frameRate: 6 },
  { key: ANIM.slimeMove, frames: [TEX.slimeStand, TEX.slimeMove], frameRate: 4 },
  { key: ANIM.snailMove, frames: [TEX.snailStand, TEX.snailMove], frameRate: 3 },
  { key: ANIM.mouseMove, frames: [TEX.mouseStand, TEX.mouseMove], frameRate: 8 },
  { key: ANIM.flyMove, frames: [TEX.flyStand, TEX.flyMove], frameRate: 10 },
];

/** What the tinted mobs draw from: a grey copy of each mob texture, and of each mob animation over the grey frames. */
export function greyMobArt(): { textures: string[]; animations: Anim[] } {
  const moves = new Set(mobSprites.map((s) => s.move));
  const anims = animations.filter((a) => moves.has(a.key));
  const textures = new Set([
    ...mobSprites.flatMap((s) => [s.stand, s.dead]),
    ...anims.flatMap((a) => a.frames),
  ]);
  return {
    textures: [...textures],
    animations: anims.map((a) => ({ ...a, key: greyKey(a.key), frames: a.frames.map(greyKey) })),
  };
}

export class Preloader extends Phaser.Scene {
  constructor() {
    super(SCENES.Preloader);
  }

  preload(): void {
    for (const a of images) {
      this.load.image(a.key, `assets/${a.file}`);
    }
    for (const id of MAP_IDS) {
      this.load.json(mapKey(id), mapFile(id));
    }
  }

  create(): void {
    const grey = greyMobArt();
    addGreyCopies(this, grey.textures);
    for (const a of [...animations, ...grey.animations]) {
      if (!this.anims.exists(a.key)) {
        this.anims.create({
          key: a.key,
          frames: a.frames.map((key) => ({ key })),
          frameRate: a.frameRate,
          repeat: -1,
        });
      }
    }
    drawPlaceholders(this);
    this.registry.set(REGISTRY.assetsReady, true);
    this.game.events.emit(EVENTS.assetsReady);
  }
}
