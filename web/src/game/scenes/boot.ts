// Boot: the first scene. It has nothing to load yet; it hands over to the
// Preloader, which fetches the art while the player is on the React screens.
import Phaser from 'phaser';

import { SCENES } from '../keys';

export class Boot extends Phaser.Scene {
  constructor() {
    super(SCENES.Boot);
  }

  create(): void {
    this.scene.start(SCENES.Preloader);
  }
}
