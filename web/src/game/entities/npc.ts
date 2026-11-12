import type Phaser from 'phaser';

import type { Marker, Npc } from '@/progression/quests';

import { DEPTH } from '../depth';
import { ART_SCALE, TEX } from '../keys';
import type { TextureKey } from '../keys';

import { NAME_TAG_ABOVE, NAME_TAG_STYLE, OUTLINE, tagBox } from './name-tag';
import type { TagBox } from './name-tag';

interface NpcArt {
  readonly texture: TextureKey;
  readonly tint: number;
}

/** No NPC art yet: a tinted player figure per look, so NPCs read apart from players. */
const looks: Readonly<Record<string, NpcArt>> = {
  guide: { texture: TEX.alienBlueStand, tint: 0xffd27a },
  trainer_warrior: { texture: TEX.alienBlueStand, tint: 0xff9a8a },
  trainer_magician: { texture: TEX.alienBlueStand, tint: 0xb8a0ff },
  trainer_bowman: { texture: TEX.alienGreenStand, tint: 0xb8f0a0 },
  trainer_thief: { texture: TEX.alienGreenStand, tint: 0x8a8aa8 },
};
const fallback: NpcArt = { texture: TEX.alienBlueStand, tint: 0xd8c8f0 };

export function npcArt(look: string): NpcArt {
  return looks[look] ?? fallback;
}

const MARKER_ABOVE = 88;

/** What each marker shows: a quest to hand in, one to take, one under way. */
export const MARKERS: Readonly<Record<Marker, { readonly text: string; readonly color: string }>> =
  {
    ready: { text: '?', color: '#f0c93a' },
    available: { text: '!', color: '#f0c93a' },
    active: { text: '?', color: '#a3a9b8' },
  };

export class NpcView {
  private readonly sprite: Phaser.GameObjects.Image;
  private readonly label: Phaser.GameObjects.Text;
  private readonly marker: Phaser.GameObjects.Text;

  constructor(scene: Phaser.Scene, npc: Npc) {
    const art = npcArt(npc.look);
    this.sprite = scene.add
      .image(npc.x, npc.y, art.texture)
      .setOrigin(0.5, 1)
      .setScale(ART_SCALE)
      .setTint(art.tint)
      .setDepth(DEPTH.actor);
    this.label = scene.add
      .text(npc.x, npc.y - NAME_TAG_ABOVE, npc.name, { ...NAME_TAG_STYLE, color: '#ffe8a8' })
      .setOrigin(0.5, 1)
      .setDepth(DEPTH.actor);
    this.marker = scene.add
      .text(npc.x, npc.y - MARKER_ABOVE, '', {
        fontSize: '20px',
        fontStyle: 'bold',
        color: MARKERS.available.color,
        fontFamily: 'sans-serif',
        ...OUTLINE,
      })
      .setOrigin(0.5, 1)
      .setDepth(DEPTH.actor)
      .setVisible(false);
  }

  setMarker(m: Marker | null): void {
    if (m === null) {
      this.marker.setVisible(false);
      return;
    }
    this.marker.setText(MARKERS[m].text).setColor(MARKERS[m].color).setVisible(true);
  }

  /** Its name tag and any marker shown over it, which players' tags keep clear of. */
  tags(): TagBox[] {
    return this.marker.visible ? [tagBox(this.label), tagBox(this.marker)] : [tagBox(this.label)];
  }

  destroy(): void {
    this.sprite.destroy();
    this.label.destroy();
    this.marker.destroy();
  }
}
