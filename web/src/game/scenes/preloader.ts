// Preloader: loads the Kenney art and the three maps in the background
// while the player logs in, defines the animations, draws the item
// placeholders, then marks the assets ready for the World scene.
import Phaser from 'phaser';

import { images, mapFile } from '../assets';
import { ANIM, MAP_IDS, SCENES, TEX, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';

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
    for (const a of animations) {
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
