import type Phaser from 'phaser';

import { mobs } from '@/gamedata/mobs';

import { ANIM, ART_SCALE, TEX } from '../keys';
import type { TextureKey } from '../keys';
import type { MobState } from '../systems/combat-view';

interface Sprite {
  readonly stand: TextureKey;
  readonly move: string; // animation key
  readonly dead: TextureKey;
}

interface MobArt extends Sprite {
  readonly tint: number | null;
  readonly scale: number; // times the art's usual size
  readonly elite: boolean; // its HP bar has a gold frame
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

// A mob from a newer server: magenta so it cannot pass for a real one.
const UNKNOWN_MOB: MobArt = { ...sprites.slime, tint: 0xff00ff, scale: 1, elite: false };

export function mobArt(type: string): MobArt {
  const mob = mobs.find((m) => m.id === type);
  if (!mob) {
    return UNKNOWN_MOB;
  }
  const { sprite, tint, scale } = mob.look;
  return { ...sprites[sprite], tint, scale, elite: mob.elite };
}

const BAR_W = 32;
const BAR_H = 4;
const BAR_ABOVE = 40;
const ELITE_FRAME = 0xf2c14e;

export class MobView {
  readonly sprite: Phaser.GameObjects.Sprite;
  private readonly bar: Phaser.GameObjects.Graphics;
  private readonly art: MobArt;
  private lastX = Number.NaN;

  constructor(scene: Phaser.Scene, type: string) {
    this.art = mobArt(type);
    this.sprite = scene.add
      .sprite(0, 0, this.art.stand)
      .setOrigin(0.5, 1)
      .setScale(ART_SCALE * this.art.scale);
    if (this.art.tint !== null) {
      this.sprite.setTint(this.art.tint);
    }
    this.bar = scene.add.graphics();
  }

  apply(s: MobState): void {
    const moving = !Number.isNaN(this.lastX) && s.x !== this.lastX;
    this.lastX = s.x;
    this.sprite.setPosition(s.x, s.y);
    this.sprite.setFlipX(s.facing > 0); // the Kenney enemies face left
    if (s.hp <= 0) {
      this.sprite.stop();
      this.sprite.setTexture(this.art.dead);
    } else if (moving) {
      this.sprite.play(this.art.move, true);
    } else {
      this.sprite.stop();
      this.sprite.setTexture(this.art.stand);
    }
    this.bar.clear();
    const x = s.x - BAR_W / 2;
    const y = s.y - BAR_ABOVE * this.art.scale;
    if (this.art.elite) {
      this.bar.lineStyle(1, ELITE_FRAME, 1).strokeRect(x - 1, y - 1, BAR_W + 2, BAR_H + 2);
    }
    this.bar.fillStyle(0x1a1a24, 0.8).fillRect(x, y, BAR_W, BAR_H);
    const ratio = s.maxHp > 0 ? Math.max(0, Math.min(1, s.hp / s.maxHp)) : 0;
    this.bar.fillStyle(0xe04848, 1).fillRect(x, y, BAR_W * ratio, BAR_H);
  }

  destroy(): void {
    this.sprite.destroy();
    this.bar.destroy();
  }
}
