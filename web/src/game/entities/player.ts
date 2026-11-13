import type Phaser from 'phaser';

import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';

import { DEPTH } from '../depth';
import { ANIM, ART_SCALE, TEX, greyKey } from '../keys';
import type { TextureKey } from '../keys';

import { poseAt } from './beats';
import type { Beats } from './beats';
import { NAME_TAG_ABOVE, NAME_TAG_STYLE } from './name-tag';
import type { TagBox } from './name-tag';
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

const LEAN_DEG = 15;
const WIND_UP_DEG = 8;
const LUNGE_PX = 8; // drawn only: the body stays where the sim has it
const STRETCH = 0.12; // wider at the strike's start, and shorter by two thirds of it
const FLASH_MS = 120;
// Phaser.TintModes' values: this file takes only Phaser's types, so tests can draw it without Phaser.
const MULTIPLY = 0;
const FILL = 1;

export class PlayerView {
  readonly sprite: Phaser.GameObjects.Sprite;
  readonly label: Phaser.GameObjects.Text;
  /** Where its feet stand, as the sim has them, whatever the pose draws: the camera follows it. */
  readonly feet = { x: 0, y: 0 };
  private cls: string;
  private art: ClassArt;
  private beats: Beats | null = null;
  private beatsFacing: 1 | -1 | null = null; // the facing the pose was first drawn with
  private flash: { readonly color: number; readonly from: number } | null = null;
  private fill: number | null = null; // the colour the sprite is filled with now; null shows the class's own tint
  private dead = false;

  constructor(scene: Phaser.Scene, cls: string, name: string) {
    this.cls = cls;
    this.art = classArt(cls);
    this.sprite = scene.add
      .sprite(0, 0, this.art.stand)
      .setOrigin(0.5, 1)
      .setScale(ART_SCALE)
      .setDepth(DEPTH.player);
    if (this.art.tint !== null) {
      this.sprite.setTint(this.art.tint);
    }
    this.label = scene.add
      .text(0, 0, name, { ...NAME_TAG_STYLE, color: '#f4f4f8' })
      .setOrigin(0.5, 1)
      .setDepth(DEPTH.player);
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
    this.fill = null;
    this.classTint();
  }

  /** Draws an action's beats from now on, in place of the last one's; a skill flashes the caster in its colour at the strike. */
  act(b: Beats, flash?: number): void {
    this.beats = b;
    this.beatsFacing = null;
    this.flash = flash === undefined ? null : { color: flash, from: b.hit };
  }

  /** Takes back the beats of an action a refusal undid, unless a newer one replaced them. */
  forget(b: Beats): void {
    if (this.beats === b) {
      this.beats = null;
    }
  }

  /** now, on the scene's clock, draws the action's beats; a skill's slow walks and climbs at the casting pace. Turning ends the pose, so its lunge never jumps to the other side. */
  apply(s: PlayerState, now: number): void {
    const facing = s.facing < 0 ? -1 : 1;
    if (this.beats) {
      this.beatsFacing ??= facing;
      if (this.beatsFacing !== facing) {
        this.beats = null;
      }
    }
    const pose = this.dead || !this.beats ? null : poseAt(this.beats, now);
    this.sprite.anims.timeScale = pose?.slowed ? movement.casting_speed : 1;
    this.feet.x = s.x;
    this.feet.y = s.y;
    this.sprite.setPosition(s.x + facing * LUNGE_PX * (pose?.lunge ?? 0), s.y);
    this.sprite.setFlipX(facing < 0);
    const stretch = STRETCH * (pose?.stretch ?? 0);
    this.sprite.setScale(ART_SCALE * (1 + stretch), ART_SCALE * (1 - (stretch * 2) / 3));
    this.label.setPosition(s.x, s.y - NAME_TAG_ABOVE);
    this.setFill(this.dead ? null : this.fillAt(now));
    if (this.dead) {
      this.sprite.stop();
      this.sprite.setTexture(this.art.hit);
      this.sprite.setAngle(facing < 0 ? 90 : -90);
      return;
    }
    const lean = pose ? (pose.lean >= 0 ? LEAN_DEG : WIND_UP_DEG) * pose.lean : 0;
    this.sprite.setAngle(facing * lean);
    if (pose?.hitFrame) {
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

  /** Its name tag where it stands, before any lift; null while hidden. */
  nameTag(): TagBox | null {
    const l = this.label;
    if (!l.visible) {
      return null;
    }
    return { x: this.feet.x, bottom: this.feet.y - NAME_TAG_ABOVE, w: l.width, h: l.height };
  }

  liftTag(bottom: number): void {
    this.label.setY(bottom);
  }

  setVisible(on: boolean): void {
    this.sprite.setVisible(on);
    this.label.setVisible(on);
  }

  setDead(dead: boolean): void {
    this.dead = dead;
  }

  setAlpha(alpha: number): void {
    this.sprite.setAlpha(alpha);
  }

  destroy(): void {
    this.sprite.destroy();
    this.label.destroy();
  }

  /** The skill's colour at its strike, else none. */
  private fillAt(now: number): number | null {
    if (this.flash && now >= this.flash.from && now < this.flash.from + FLASH_MS) {
      return this.flash.color;
    }
    return null;
  }

  private setFill(fill: number | null): void {
    if (fill === this.fill) {
      return;
    }
    this.fill = fill;
    if (fill === null) {
      this.classTint();
    } else {
      this.sprite.setTint(fill).setTintMode(FILL);
    }
  }

  private classTint(): void {
    this.sprite.setTintMode(MULTIPLY);
    if (this.art.tint === null) {
      this.sprite.clearTint();
    } else {
      this.sprite.setTint(this.art.tint);
    }
  }
}
