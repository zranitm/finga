import type Phaser from 'phaser';

import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';

import { DEPTH } from '../depth';
import { UNSEEN_ALPHA, warmOnce } from '../effects/draw';
import { ANIM, ART_SCALE, TEX, greyKey } from '../keys';
import type { MobState } from '../systems/combat-view';
import type { Pose, Swing } from '../systems/mob-attacks';

interface Sprite {
  readonly stand: string; // texture key
  readonly move: string; // animation key
  readonly dead: string;
}

interface MobArt extends Sprite {
  readonly tint: number | null;
  readonly scale: number; // times the art's usual size
  readonly elite: boolean; // its HP bar has a gold frame
  readonly boss: boolean; // the boss bar at the top shows its HP, so it has none overhead
  readonly superArmor: boolean; // a faint warm outline: it won't stagger
}

type SpriteName = (typeof mobs)[number]['look']['sprite'];

const sprites = {
  slime: { stand: TEX.slimeStand, move: ANIM.slimeMove, dead: TEX.slimeDead },
  snail: { stand: TEX.snailStand, move: ANIM.snailMove, dead: TEX.snailDead },
  mouse: { stand: TEX.mouseStand, move: ANIM.mouseMove, dead: TEX.mouseDead },
  fly: { stand: TEX.flyStand, move: ANIM.flyMove, dead: TEX.flyDead },
};

// A sprite a mob names but this table lacks fails the type check.
const _sprites: Record<SpriteName, Sprite> = sprites;
void _sprites;

/** Every sprite mobs draw from; the preloader makes each a grey copy for the tinted ones. */
export const mobSprites: readonly Sprite[] = Object.values(sprites);

const grey = (s: Sprite): Sprite => ({
  stand: greyKey(s.stand),
  move: greyKey(s.move),
  dead: greyKey(s.dead),
});

// A mob from a newer server: magenta so it cannot pass for a real one.
const UNKNOWN_MOB: MobArt = {
  ...grey(sprites.slime),
  tint: 0xff00ff,
  scale: 1,
  elite: false,
  boss: false,
  superArmor: false,
};

/** A tinted mob draws from the grey copy of its sprite, so the tint is its colour. */
export function mobArt(type: string): MobArt {
  const mob = mobs.find((m) => m.id === type);
  if (!mob) {
    return UNKNOWN_MOB;
  }
  const { sprite, tint, scale } = mob.look;
  return {
    ...(tint === null ? sprites[sprite] : grey(sprites[sprite])),
    tint,
    scale,
    elite: mob.elite,
    boss: mob.boss,
    superArmor: mob.super_armor,
  };
}

const BAR_W = 32;
const BAR_H = 4;
const BAR_ABOVE = 40;
const ELITE_FRAME = 0xf2c14e;

const SQUASH = 0.2; // a windup's crouch, as a share of the height
const SHAKE_PX = 1.5;
const LUNGE_PX = 8;
const LEAP_PX = 24; // a hop's height at its middle, times the mob's scale
const LEAN = 0.15; // radians a charge leans forward
const FX = 0xfff4d6;

const MULTIPLY = 0; // tint modes
const FILL = 1;
const FLASH_MS = 120; // a flinch's white flash
const JOLT_PX = 4; // a flinch's jolt back, easing out over JOLT_MS
const JOLT_MS = 150;
const PUFF_MS = 300; // the dust as a mob goes down
const BLINK_MS = 100; // a rising mob's blink
const ARMOR = 0xffc070; // super armor's outline

/** Super armor's faint warm outline, WebGL only. */
function armor(s: Phaser.GameObjects.Sprite): void {
  s.enableFilters().filters?.external.addGlow(ARMOR, 2, 0, 1, false, 8, 6);
}

/** An armored outline drawn once, unseen, among the mobs: the first elite or boss then finds its shader and passes built, and doesn't hitch. */
export function warmArmor(scene: Phaser.Scene, x: number, y: number): void {
  const s = scene.add
    .sprite(x, y, sprites.slime.stand)
    .setOrigin(0.5, 1)
    .setScale(ART_SCALE)
    .setAlpha(UNSEEN_ALPHA)
    .setDepth(DEPTH.actor);
  armor(s);
  warmOnce(scene, s);
}

/** An attack under way as drawn: the swing and where it is. */
export interface Attacking {
  readonly swing: Swing;
  readonly pose: Pose;
}

export class MobView {
  readonly sprite: Phaser.GameObjects.Sprite;
  /** Where its feet stand, as the server has it, whatever its pose draws: a hit's spark and its attack's look go there. */
  readonly feet = { x: 0, y: 0 };
  private readonly bar: Phaser.GameObjects.Graphics;
  private readonly fx: Phaser.GameObjects.Graphics;
  private readonly art: MobArt;
  private readonly box: { readonly w: number; readonly h: number };
  private lastX = Number.NaN;
  private heldUntil = -Infinity; // a hit-stop holds its frame till then

  constructor(
    scene: Phaser.Scene,
    readonly type: string,
  ) {
    this.art = mobArt(type);
    const def = mobs.find((m) => m.id === type);
    this.box = { w: def?.width ?? BAR_W, h: def?.height ?? BAR_W };
    this.sprite = scene.add
      .sprite(0, 0, this.art.stand)
      .setOrigin(0.5, 1)
      .setScale(ART_SCALE * this.art.scale)
      .setDepth(DEPTH.actor);
    this.bar = scene.add.graphics().setDepth(DEPTH.actor);
    this.fx = scene.add.graphics().setDepth(DEPTH.effect);
    if (this.art.superArmor) {
      armor(this.sprite);
    }
  }

  /** Holds its animation's frame until then, as the player's own hit lands; it keeps its place meanwhile. */
  hold(until: number): void {
    this.heldUntil = Math.max(this.heldUntil, until);
  }

  /** Draws the mob as the server last placed it, taking a blow or making the attack it's in, if any. */
  apply(s: MobState, now: number, attacking?: Attacking): void {
    const moving = !Number.isNaN(this.lastX) && s.x !== this.lastX;
    this.lastX = s.x;
    this.feet.x = s.x;
    this.feet.y = s.y;
    this.sprite.setPosition(s.x, s.y);
    this.sprite
      .setScale(ART_SCALE * this.art.scale)
      .setRotation(0)
      .setAlpha(1);
    this.artTint();
    this.sprite.setFlipX(s.facing > 0); // the Kenney enemies face left
    if (now < this.heldUntil) {
      this.sprite.anims.pause();
    } else if (s.hp <= 0) {
      this.sprite.stop();
      this.sprite.setTexture(this.art.dead);
    } else if (moving) {
      this.sprite.anims.resume();
      this.sprite.play(this.art.move, true);
    } else {
      this.sprite.stop();
      this.sprite.setTexture(this.art.stand);
    }
    this.fx.clear();
    if (s.hp > 0) {
      this.react(s);
      if (attacking) {
        this.pose(attacking);
      }
    }
    this.bar.clear();
    if (this.art.boss) {
      return;
    }
    const x = s.x - BAR_W / 2;
    const y = s.y - BAR_ABOVE * this.art.scale;
    if (this.art.elite) {
      this.bar.lineStyle(1, ELITE_FRAME, 1).strokeRect(x - 1, y - 1, BAR_W + 2, BAR_H + 2);
    }
    this.bar.fillStyle(0x1a1a24, 0.8).fillRect(x, y, BAR_W, BAR_H);
    const ratio = s.maxHp > 0 ? Math.max(0, Math.min(1, s.hp / s.maxHp)) : 0;
    this.bar.fillStyle(0xe04848, 1).fillRect(x, y, BAR_W * ratio, BAR_H);
  }

  /** The windup's crouch and shake, then the act of the attack's kind; its ring, trail and impact are its effect (effects/mob-looks). */
  private pose({ swing, pose }: Attacking): void {
    const k = ART_SCALE * this.art.scale;
    const { attack, facing } = swing;
    const p = pose.progress;
    const fade = 1 - p;
    switch (pose.phase) {
      case 'windup':
        this.sprite.setScale(k * (1 + SQUASH * 0.6 * p), k * (1 - SQUASH * p));
        this.sprite.x += Math.sin(p * 40) * SHAKE_PX;
        return;
      case 'travel':
        if (attack.kind === 'leap') {
          this.sprite.y -= LEAP_PX * this.art.scale * 4 * p * fade;
        } else {
          this.sprite.setRotation(facing * LEAN); // its top forward: clockwise facing right
        }
        return;
      case 'impact':
        switch (attack.kind) {
          case 'front':
            this.sprite.x += facing * LUNGE_PX * fade;
            return;
          case 'leap':
            this.sprite.setScale(k * (1 + SQUASH * fade), k * (1 - SQUASH * fade));
            return;
          case 'around':
            this.sprite.setScale(k * (1 + SQUASH * 1.5 * fade), k * (1 - SQUASH * 2 * fade));
            return;
          case 'shot':
            this.sprite.x -= facing * 3 * fade;
            return;
          default:
            return;
        }
    }
  }

  /** A blow taken: a flinch's white flash and jolt back, lying down in a puff of dust, or a rising blink. */
  private react(s: MobState): void {
    const { w, h } = this.box;
    switch (s.pose) {
      case MobPose.FLINCH:
        if (s.poseMs < FLASH_MS) {
          this.sprite.setTint(0xffffff).setTintMode(FILL);
        }
        this.sprite.x -= s.facing * JOLT_PX * Math.max(0, 1 - s.poseMs / JOLT_MS);
        return;
      case MobPose.DOWN: {
        this.sprite.setRotation(-s.facing * (Math.PI / 2)); // on its back, its feet toward where it faced
        this.sprite.x += s.facing * (h / 2);
        this.sprite.y -= w / 2;
        const p = s.poseMs / PUFF_MS;
        if (p < 1) {
          this.fx
            .fillStyle(FX, 0.5 * (1 - p))
            .fillEllipse(s.x, s.y - 2, (w + h) * (0.6 + 0.6 * p), 10);
        }
        return;
      }
      case MobPose.RISING:
        this.sprite.setAlpha(Math.floor(s.poseMs / BLINK_MS) % 2 === 0 ? 0.4 : 1);
        return;
      default:
        return;
    }
  }

  private artTint(): void {
    this.sprite.setTintMode(MULTIPLY);
    if (this.art.tint === null) {
      this.sprite.clearTint();
    } else {
      this.sprite.setTint(this.art.tint);
    }
  }

  destroy(): void {
    this.sprite.destroy();
    this.bar.destroy();
    this.fx.destroy();
  }
}
