import type Phaser from 'phaser';

import { ANIM, ART_SCALE, TEX } from '../keys';
import type { TextureKey } from '../keys';

import { NAME_TAG_ABOVE } from './name-tag';
import { climbMoving, poseFor } from './pose';
import type { PoseState } from './pose';

export interface PlayerState extends PoseState {
  readonly x: number; // feet, as in sim/step
  readonly y: number;
  readonly facing: 1 | -1;
}

export interface ClassArt {
  readonly stand: TextureKey;
  readonly jump: TextureKey;
  readonly climb: TextureKey;
  readonly hit: TextureKey; // the swing, and lying dead
  readonly walk: string; // animation key
  readonly climbAnim: string;
}

const warrior: ClassArt = {
  stand: TEX.warriorStand,
  jump: TEX.warriorJump,
  climb: TEX.warriorClimb1,
  hit: TEX.warriorHit,
  walk: ANIM.warriorWalk,
  climbAnim: ANIM.warriorClimb,
};

const art: Readonly<Record<string, ClassArt>> = {
  warrior,
  bowman: {
    stand: TEX.bowmanStand,
    jump: TEX.bowmanJump,
    climb: TEX.bowmanClimb1,
    hit: TEX.bowmanHit,
    walk: ANIM.bowmanWalk,
    climbAnim: ANIM.bowmanClimb,
  },
};

export function classArt(cls: string): ClassArt {
  return art[cls] ?? warrior;
}

const SWING_MS = 150;

export class PlayerView {
  readonly sprite: Phaser.GameObjects.Sprite;
  readonly label: Phaser.GameObjects.Text | null;
  private readonly art: ClassArt;
  private swingUntil = Number.NEGATIVE_INFINITY;
  private dead = false;

  constructor(scene: Phaser.Scene, cls: string, name?: string) {
    this.art = classArt(cls);
    this.sprite = scene.add.sprite(0, 0, this.art.stand).setOrigin(0.5, 1).setScale(ART_SCALE);
    this.label =
      name === undefined
        ? null
        : scene.add
            .text(0, 0, name, { fontSize: '12px', color: '#f4f4f8', fontFamily: 'sans-serif' })
            .setOrigin(0.5, 1);
  }

  /** `now` lets a recent attack keep its swing pose. */
  apply(s: PlayerState, now = Number.NEGATIVE_INFINITY): void {
    this.sprite.setPosition(s.x, s.y);
    this.sprite.setFlipX(s.facing < 0);
    this.label?.setPosition(s.x, s.y - NAME_TAG_ABOVE);
    if (this.dead) {
      this.sprite.stop();
      this.sprite.setTexture(this.art.hit);
      this.sprite.setAngle(s.facing < 0 ? 90 : -90);
      return;
    }
    this.sprite.setAngle(0);
    if (now < this.swingUntil) {
      this.sprite.stop();
      this.sprite.setTexture(this.art.hit);
      return;
    }
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

  setVisible(on: boolean): void {
    this.sprite.setVisible(on);
    this.label?.setVisible(on);
  }

  attack(now: number): void {
    this.swingUntil = now + SWING_MS;
  }

  setDead(dead: boolean): void {
    this.dead = dead;
  }

  setAlpha(alpha: number): void {
    this.sprite.setAlpha(alpha);
  }

  destroy(): void {
    this.sprite.destroy();
    this.label?.destroy();
  }
}
