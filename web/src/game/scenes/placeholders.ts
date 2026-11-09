// Item templates with no Kenney match get a 32 px shape drawn at runtime: equipment by its slot kind in its stars' colour, the drop materials by hand.
import type Phaser from 'phaser';

import { items } from '@/gamedata/items';

import { DRAWN_ITEMS, TILE, UNKNOWN_ITEM } from '../keys';

const palette = {
  wood: 0x8a5a3c,
  gel: 0x7ac74f,
  orange: 0xe08a3c,
  bone: 0xf0ead6,
  dark: 0x3a3a4a,
  string: 0xe6e6e6,
} as const;

/** The item windows' --star-N colours in styles/global.css, so a drop reads as its card does. */
const starColors: Readonly<Record<number, number>> = {
  1: 0xa3a9b8,
  2: 0x5cc46a,
  3: 0x4f8fe0,
  4: 0xa86ee0,
  5: 0xf08a3a,
  6: 0xe04848,
  7: 0xf0c93a,
};

type Draw = (g: Phaser.GameObjects.Graphics) => void;

function sword(blade: number): Draw {
  return (g) => {
    g.fillStyle(blade).fillTriangle(6, 26, 24, 4, 28, 8).fillTriangle(6, 26, 28, 8, 10, 30);
    g.fillStyle(palette.wood).fillRect(4, 22, 8, 8);
    g.fillStyle(palette.dark).fillRect(8, 18, 10, 3);
  };
}

function bow(limb: number): Draw {
  return (g) => {
    g.lineStyle(3, limb).beginPath().arc(10, 16, 12, -1.2, 1.2).strokePath();
    g.lineStyle(1, palette.string).lineBetween(14, 5, 14, 27);
  };
}

/** One shape per slot kind but the weapon, which follows its class. */
const kinds: Readonly<Record<string, (c: number) => Draw>> = {
  hat: (c) => (g) => {
    g.fillStyle(c).fillEllipse(16, 18, 24, 18).fillRect(4, 18, 24, 6);
  },
  top: (c) => (g) => {
    g.fillStyle(c).fillTriangle(6, 6, 26, 6, 24, 28).fillTriangle(6, 6, 24, 28, 8, 28);
    g.fillStyle(palette.dark).fillRect(15, 8, 2, 20);
  },
  bottom: (c) => (g) => {
    g.fillStyle(c).fillRect(8, 4, 16, 8).fillRect(8, 10, 7, 18).fillRect(17, 10, 7, 18);
  },
  gloves: (c) => (g) => {
    g.fillStyle(c).fillEllipse(14, 16, 14, 18).fillEllipse(22, 16, 6, 10);
    g.fillStyle(palette.dark).fillRect(8, 24, 12, 5);
  },
  shoes: (c) => (g) => {
    g.fillStyle(c).fillRect(10, 6, 8, 18).fillRect(10, 20, 16, 8);
    g.fillStyle(palette.dark).fillRect(10, 27, 16, 2);
  },
  necklace: (c) => (g) => {
    g.lineStyle(2, palette.string)
      .beginPath()
      .arc(16, 10, 10, 0.2, Math.PI - 0.2)
      .strokePath();
    g.fillStyle(c).fillCircle(16, 24, 5);
  },
  earrings: (c) => (g) => {
    g.lineStyle(2, palette.string).lineBetween(10, 8, 10, 16).lineBetween(22, 8, 22, 16);
    g.fillStyle(c).fillCircle(10, 20, 4).fillCircle(22, 20, 4);
  },
  ring: (c) => (g) => {
    g.lineStyle(4, c).strokeCircle(16, 19, 9);
    g.fillStyle(palette.bone).fillCircle(16, 9, 4);
  },
};

const materials: Readonly<Record<string, Draw>> = {
  slime_gel: (g) => {
    g.fillStyle(palette.gel).fillEllipse(16, 20, 22, 16).fillCircle(12, 14, 6);
  },
  mushroom_cap: (g) => {
    g.fillStyle(palette.orange).fillEllipse(16, 16, 26, 18);
    g.fillStyle(palette.bone).fillCircle(10, 14, 3).fillCircle(20, 12, 3);
  },
  boar_tusk: (g) => {
    g.fillStyle(palette.bone)
      .fillTriangle(6, 26, 14, 4, 26, 10)
      .fillTriangle(6, 26, 26, 10, 14, 14);
  },
  bat_wing: (g) => {
    g.fillStyle(palette.dark).fillTriangle(4, 8, 28, 4, 22, 28).fillTriangle(4, 8, 22, 28, 8, 20);
  },
};

const unknown: Draw = (g) => {
  g.lineStyle(2, palette.string).strokeRect(7, 7, 18, 18);
  g.fillStyle(palette.string).fillCircle(16, 16, 3);
};

interface Art {
  readonly id: string;
  readonly slot?: string;
  readonly class?: string;
  readonly stars: number;
}

const catalog: readonly Art[] = items;

function drawFor(id: string): Draw {
  const item = catalog.find((i) => i.id === id);
  if (item?.slot === undefined) {
    return materials[id] ?? unknown;
  }
  const color = starColors[item.stars] ?? starColors[1]!;
  if (item.slot === 'weapon') {
    return item.class === 'bowman' ? bow(color) : sword(color);
  }
  return kinds[item.slot]?.(color) ?? unknown;
}

export function drawPlaceholders(scene: Phaser.Scene): void {
  for (const key of [...DRAWN_ITEMS, UNKNOWN_ITEM]) {
    if (scene.textures.exists(key)) {
      continue;
    }
    const g = scene.make.graphics({}, false);
    drawFor(key)(g);
    g.generateTexture(key, TILE, TILE);
    g.destroy();
  }
}
