// Loads in the background while the player logs in, then marks the assets ready for World.
import Phaser from 'phaser';

import { images, mapFile } from '../assets';
import type { Manifest } from '../effects/manifest';
import { loadEffects, readyEffects } from '../effects/play';
import { MAP_IDS, SCENES, animations, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';

import { addGreyCopies, greyArt } from './grey-art';
import { drawPlaceholders } from './placeholders';

export class Preloader extends Phaser.Scene {
  private effects: Manifest | null = null;

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
    loadEffects(this.load, (m) => {
      this.effects = m;
    });
  }

  create(): void {
    const grey = greyArt();
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
    readyEffects(this, this.effects);
    this.registry.set(REGISTRY.assetsReady, true);
    this.game.events.emit(EVENTS.assetsReady);
  }
}
