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
