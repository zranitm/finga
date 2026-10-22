// Damage numbers (C10): one Phaser text per live number from the pure
// DamageNumbers queue, rising and fading as it says.
import type Phaser from 'phaser';

import type { DrawnNumber, NumberKind } from '../systems/combat-view';

const styles: Readonly<Record<NumberKind, Phaser.Types.GameObjects.Text.TextStyle>> = {
  hit: { fontSize: '14px', color: '#f4f4f8', fontFamily: 'sans-serif', fontStyle: 'bold' },
  crit: { fontSize: '18px', color: '#ffd166', fontFamily: 'sans-serif', fontStyle: 'bold' },
  miss: { fontSize: '13px', color: '#9a9ab0', fontFamily: 'sans-serif' },
  taken: { fontSize: '14px', color: '#e04848', fontFamily: 'sans-serif', fontStyle: 'bold' },
};

export class DamageNumberLayer {
  private readonly texts = new Map<number, Phaser.GameObjects.Text>();

  constructor(private readonly scene: Phaser.Scene) {}

  /** Creates, moves and fades texts to match the live numbers; destroys the rest. */
  sync(drawn: readonly DrawnNumber[]): void {
    const live = new Set<number>();
    for (const n of drawn) {
      live.add(n.id);
      let t = this.texts.get(n.id);
      if (!t) {
        t = this.scene.add.text(0, 0, n.text, styles[n.kind]).setOrigin(0.5, 1).setDepth(10);
        this.texts.set(n.id, t);
      }
      t.setPosition(n.x, n.y + n.dy).setAlpha(n.alpha);
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
