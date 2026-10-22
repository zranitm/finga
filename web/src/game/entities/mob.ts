// A mob view (C10): the Kenney enemy for the type, tinted where the shape
// is only near (decision 119), with an HP bar above. It draws the state it
// is given and owns no rules.
import type Phaser from 'phaser';

import { ANIM, ART_SCALE, TEX } from '../keys';
import type { TextureKey } from '../keys';
import type { MobState } from '../systems/combat-view';

interface MobArt {
  readonly stand: TextureKey;
  readonly move: string; // animation key
  readonly dead: TextureKey;
  readonly tint: number | null;
}

const slime: MobArt = {
  stand: TEX.slimeStand,
  move: ANIM.slimeMove,
  dead: TEX.slimeDead,
  tint: null,
};

const art: Readonly<Record<string, MobArt>> = {
  green_slime: slime,
  orange_mushroom: {
    stand: TEX.snailStand,
    move: ANIM.snailMove,
    dead: TEX.snailDead,
    tint: 0xe08a3c,
  },
  wild_boar: { stand: TEX.mouseStand, move: ANIM.mouseMove, dead: TEX.mouseDead, tint: 0x8a5a3c },
  cave_bat: { stand: TEX.flyStand, move: ANIM.flyMove, dead: TEX.flyDead, tint: 0x9a9ab0 },
};

/** The art for a mob type; an unknown type gets the slime's rather than nothing. */
export function mobArt(type: string): MobArt {
  return art[type] ?? slime;
}

const BAR_W = 32;
const BAR_H = 4;
const BAR_ABOVE = 40; // above the feet

export class MobView {
  readonly sprite: Phaser.GameObjects.Sprite;
  private readonly bar: Phaser.GameObjects.Graphics;
  private readonly art: MobArt;
  private lastX = Number.NaN;

  constructor(scene: Phaser.Scene, type: string) {
    this.art = mobArt(type);
    this.sprite = scene.add.sprite(0, 0, this.art.stand).setOrigin(0.5, 1).setScale(ART_SCALE);
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
    const y = s.y - BAR_ABOVE;
    this.bar.fillStyle(0x1a1a24, 0.8).fillRect(x, y, BAR_W, BAR_H);
    const ratio = s.max_hp > 0 ? Math.max(0, Math.min(1, s.hp / s.max_hp)) : 0;
    this.bar.fillStyle(0xe04848, 1).fillRect(x, y, BAR_W * ratio, BAR_H);
  }

  destroy(): void {
    this.sprite.destroy();
    this.bar.destroy();
  }
}
