import type Phaser from 'phaser';

import { DEPTH } from '../depth';
import type { DrawnNumber, NumberKind } from '../systems/combat-view';

const CRIT = 1.3; // a crit's number is this much bigger

// Outlined, so it reads on grass and sky.
const healed: Phaser.Types.GameObjects.Text.TextStyle = {
  fontSize: '14px',
  fontFamily: 'sans-serif',
  fontStyle: 'bold',
  stroke: '#0a0c16',
  strokeThickness: 3,
};

const styles: Readonly<Record<NumberKind, Phaser.Types.GameObjects.Text.TextStyle>> = {
  hit: { fontSize: '14px', color: '#f4f4f8', fontFamily: 'sans-serif', fontStyle: 'bold' },
  crit: {
    fontSize: `${Math.round(14 * CRIT)}px`,
    color: '#ffd166',
    fontFamily: 'sans-serif',
    fontStyle: 'bold',
  },
  miss: { fontSize: '13px', color: '#9a9ab0', fontFamily: 'sans-serif' },
  taken: { fontSize: '14px', color: '#e04848', fontFamily: 'sans-serif', fontStyle: 'bold' },
  healHp: { ...healed, color: '#6fdc6f' },
  healMp: { ...healed, color: '#6f9cf0' },
};

export class DamageNumberLayer {
  private readonly texts = new Map<number, Phaser.GameObjects.Text>();

  constructor(private readonly scene: Phaser.Scene) {}

  sync(drawn: readonly DrawnNumber[]): void {
    const live = new Set<number>();
    for (const n of drawn) {
      live.add(n.id);
      let t = this.texts.get(n.id);
      if (!t) {
        t = this.scene.add
          .text(0, 0, n.text, styles[n.kind])
          .setOrigin(0.5, 1)
          .setDepth(DEPTH.damage);
        this.texts.set(n.id, t);
      }
      t.setPosition(n.x, n.y + n.dy)
        .setAlpha(n.alpha)
        .setScale(n.scale);
    }
    for (const [id, t] of this.texts) {
      if (!live.has(id)) {
        t.destroy();
        this.texts.delete(id);
      }
    }
  }

  destroy(): void {
    for (const t of this.texts.values()) {
      t.destroy();
    }
    this.texts.clear();
  }
}
