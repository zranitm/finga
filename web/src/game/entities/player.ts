// The player view: a sprite that draws the state it is given (position,
// facing, pose). It owns no rules; the scene passes it plain state.
import type Phaser from 'phaser';

import { ANIM, ART_SCALE, TEX } from '../keys';
import type { TextureKey } from '../keys';

import { climbMoving, poseFor } from './pose';
import type { PoseState } from './pose';

export interface PlayerState extends PoseState {
  readonly x: number; // feet, bottom centre, as in sim/step
  readonly y: number;
  readonly facing: 1 | -1;
}

export interface ClassArt {
  readonly stand: TextureKey;
  readonly jump: TextureKey;
  readonly climb: TextureKey;
  readonly walk: string; // animation key
  readonly climbAnim: string;
}

const warrior: ClassArt = {
  stand: TEX.warriorStand,
  jump: TEX.warriorJump,
  climb: TEX.warriorClimb1,
  walk: ANIM.warriorWalk,
  climbAnim: ANIM.warriorClimb,
};

const art: Readonly<Record<string, ClassArt>> = {
  warrior,
  bowman: {
    stand: TEX.bowmanStand,
    jump: TEX.bowmanJump,
    climb: TEX.bowmanClimb1,
    walk: ANIM.bowmanWalk,
    climbAnim: ANIM.bowmanClimb,
  },
};

/** The art for a class id; an unknown class gets the warrior's rather than nothing. */
export function classArt(cls: string): ClassArt {
  return art[cls] ?? warrior;
}

export class PlayerView {
  readonly sprite: Phaser.GameObjects.Sprite;
  private readonly art: ClassArt;

  constructor(scene: Phaser.Scene, cls: string) {
    this.art = classArt(cls);
    this.sprite = scene.add.sprite(0, 0, this.art.stand).setOrigin(0.5, 1).setScale(ART_SCALE);
  }

  apply(s: PlayerState): void {
    this.sprite.setPosition(s.x, s.y);
    this.sprite.setFlipX(s.facing < 0);
    switch (poseFor(s)) {
      case 'walk':
        this.sprite.play(this.art.walk, true);
        return;
      case 'climb':
        if (climbMoving(s)) {
          this.sprite.play(this.art.climbAnim, true);
        } else {
          this.sprite.stop();
          this.sprite.setTexture(this.art.climb);
        }
        return;
      case 'jump':
        this.sprite.stop();
        this.sprite.setTexture(this.art.jump);
        return;
      case 'stand':
        this.sprite.stop();
        this.sprite.setTexture(this.art.stand);
        return;
    }
  }

  destroy(): void {
    this.sprite.destroy();
  }
}
