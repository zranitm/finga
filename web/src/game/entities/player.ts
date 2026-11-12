import type Phaser from 'phaser';

import { classes } from '@/gamedata/classes';

import { ANIM, ART_SCALE, TEX, greyKey } from '../keys';
import type { TextureKey } from '../keys';

import { NAME_TAG_ABOVE } from './name-tag';
import { climbMoving, poseFor } from './pose';
import type { PoseState } from './pose';

export interface PlayerState extends PoseState {
  readonly x: number; // feet, as in sim/step
  readonly y: number;
  readonly facing: 1 | -1;
}

interface Sprite {
  readonly stand: TextureKey;
  readonly jump: TextureKey;
  readonly climb: TextureKey;
  readonly hit: TextureKey; // the swing, and lying dead
  readonly walk: string; // animation key
  readonly climbAnim: string;
}

export interface ClassArt {
  readonly stand: string;
  readonly jump: string;
  readonly climb: string;
  readonly hit: string;
  readonly walk: string;
  readonly climbAnim: string;
  readonly tint: number | null;
}

type SpriteName = (typeof classes)[number]['look']['sprite'];

const sprites = {
  alien_blue: {
    stand: TEX.alienBlueStand,
    jump: TEX.alienBlueJump,
    climb: TEX.alienBlueClimb1,
    hit: TEX.alienBlueHit,
    walk: ANIM.alienBlueWalk,
    climbAnim: ANIM.alienBlueClimb,
  },
  alien_green: {
    stand: TEX.alienGreenStand,
    jump: TEX.alienGreenJump,
    climb: TEX.alienGreenClimb1,
    hit: TEX.alienGreenHit,
    walk: ANIM.alienGreenWalk,
    climbAnim: ANIM.alienGreenClimb,
  },
};

// A sprite a class names but this table lacks fails the type check.
const _sprites: Record<SpriteName, Sprite> = sprites;
void _sprites;

/** Every sprite players draw from; the preloader makes each a grey copy for the tinted classes. */
export const playerSprites: readonly Sprite[] = Object.values(sprites);

const grey = (s: Sprite): Omit<ClassArt, 'tint'> => ({
  stand: greyKey(s.stand),
  jump: greyKey(s.jump),
  climb: greyKey(s.climb),
  hit: greyKey(s.hit),
  walk: greyKey(s.walk),
  climbAnim: greyKey(s.climbAnim),
});

// A class from a newer server: magenta so it cannot pass for a real one.
const UNKNOWN_CLASS: ClassArt = { ...grey(sprites.alien_blue), tint: 0xff00ff };

/** A class draws its look: a tinted one from the grey copy of its sprite, so the tint is its colour. */
export function classArt(cls: string): ClassArt {
  const c = classes.find((k) => k.id === cls);
  if (!c) {
    return UNKNOWN_CLASS;
  }
  const { sprite, tint } = c.look;
  return { ...(tint === null ? sprites[sprite] : grey(sprites[sprite])), tint };
}

const SWING_MS = 150;

export class PlayerView {
  readonly sprite: Phaser.GameObjects.Sprite;
  readonly label: Phaser.GameObjects.Text | null;
  private cls: string;
  private art: ClassArt;
  private swingUntil = Number.NEGATIVE_INFINITY;
  private dead = false;

  constructor(scene: Phaser.Scene, cls: string, name?: string) {
    this.cls = cls;
    this.art = classArt(cls);
    this.sprite = scene.add.sprite(0, 0, this.art.stand).setOrigin(0.5, 1).setScale(ART_SCALE);
    if (this.art.tint !== null) {
      this.sprite.setTint(this.art.tint);
    }
    this.label =
      name === undefined
        ? null
        : scene.add
            .text(0, 0, name, { fontSize: '12px', color: '#f4f4f8', fontFamily: 'sans-serif' })
            .setOrigin(0.5, 1);
  }

  /** Draws the class's look from now on, as after a promotion; the next apply picks the pose. */
  setClass(cls: string): void {
    if (cls === this.cls) {
      return;
    }
    this.cls = cls;
    this.art = classArt(cls);
    this.sprite.stop();
    this.sprite.setTexture(this.art.stand);
    if (this.art.tint === null) {
      this.sprite.clearTint();
    } else {
      this.sprite.setTint(this.art.tint);
    }
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
