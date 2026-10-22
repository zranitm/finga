// A drop on the ground (C11): the item's texture (a tinted flask for
// potions, a placeholder for the rest), a bounce when it appears, and a
// hint when the player is close enough to pick it up.
import type Phaser from 'phaser';

import { ART_SCALE, ITEM_TINTS, itemTexture } from '../keys';

const BOUNCE_PX = 24;
const BOUNCE_MS = 400;

export class DropView {
  readonly sprite: Phaser.GameObjects.Sprite;
  private hinted = false;

  constructor(scene: Phaser.Scene, template: string, x: number, y: number) {
    const texture = itemTexture(template);
    this.sprite = scene.add.sprite(x, y - BOUNCE_PX, texture).setOrigin(0.5, 1);
    if (texture === 'flask') {
      this.sprite.setScale(ART_SCALE * 1.4); // 68x93 px art down to about the grid
    }
    const tint = ITEM_TINTS[template];
    if (tint !== undefined) {
      this.sprite.setTint(tint);
    }
    scene.tweens.add({ targets: this.sprite, y, duration: BOUNCE_MS, ease: 'Bounce.easeOut' });
  }

  /** Within pickup range: a little larger and brighter. */
  setHint(on: boolean): void {
    if (on === this.hinted) {
      return;
    }
    this.hinted = on;
    const base = this.sprite.scaleX / (this.hinted ? 1 : 1.25);
    this.sprite.setScale(on ? base * 1.25 : base);
    this.sprite.setAlpha(on ? 1 : 0.9);
  }

  destroy(): void {
    this.sprite.destroy();
  }
}
