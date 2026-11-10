import type Phaser from 'phaser';

import { ART_SCALE, ITEM_TINTS, itemTexture } from '../keys';
import type { Point } from '../systems/drops';

const BOUNCE_PX = 24;
const BOUNCE_MS = 400;
const FLY_MS = 300;
const FADE_MS = 500;

export class DropView {
  readonly sprite: Phaser.GameObjects.Sprite;

  constructor(
    private readonly scene: Phaser.Scene,
    template: string,
    x: number,
    y: number,
  ) {
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

  /** Flies into its owner at to, shrinking, then goes. */
  flyTo(to: Point): void {
    this.scene.tweens.killTweensOf(this.sprite);
    this.scene.tweens.add({
      targets: this.sprite,
      x: to.x,
      y: to.y,
      scale: this.sprite.scale * 0.5,
      alpha: 0.4,
      duration: FLY_MS,
      ease: 'Quad.easeIn',
      onComplete: () => this.destroy(),
    });
  }

  /** Fades where it lies, then goes. */
  fade(): void {
    this.scene.tweens.killTweensOf(this.sprite);
    this.scene.tweens.add({
      targets: this.sprite,
      alpha: 0,
      duration: FADE_MS,
      onComplete: () => this.destroy(),
    });
  }

  destroy(): void {
    this.sprite.destroy();
  }
}
