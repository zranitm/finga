// Item templates with no Kenney match get a 32 px shape drawn at runtime: equipment by its slot kind in its stars' colour, the drop materials from a few shapes in their mob's colours.
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

function gel(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillEllipse(16, 20, 22, 16).fillCircle(12, 14, 6);
  };
}

function shell(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillCircle(16, 18, 11);
    g.lineStyle(2, palette.dark).strokeCircle(16, 18, 6);
    g.fillStyle(palette.dark).fillCircle(16, 18, 2);
  };
}

function dust(c: number): Draw {
  return (g) => {
    g.fillStyle(c);
    for (const [x, y, r] of [
      [10, 12, 3],
      [20, 9, 2],
      [16, 18, 4],
      [24, 20, 3],
      [9, 23, 2],
      [19, 26, 2],
    ] as const) {
      g.fillCircle(x, y, r);
    }
  };
}

function tail(c: number): Draw {
  return (g) => {
    g.lineStyle(5, c).beginPath().arc(20, 14, 10, 1.6, 4.4).strokePath();
    g.fillStyle(palette.bone).fillCircle(12, 12, 2).fillCircle(13, 20, 2);
  };
}

function whiskers(c: number): Draw {
  return (g) => {
    g.lineStyle(2, c)
      .lineBetween(4, 10, 28, 14)
      .lineBetween(4, 18, 28, 16)
      .lineBetween(4, 26, 28, 18);
  };
}

function feather(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillEllipse(16, 15, 12, 24);
    g.lineStyle(2, palette.bone).lineBetween(16, 4, 16, 30);
  };
}

function fang(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillTriangle(10, 6, 22, 6, 16, 28);
    g.fillStyle(palette.dark).fillRect(10, 4, 12, 3);
  };
}

function orb(c: number, rim: number): Draw {
  return (g) => {
    g.fillStyle(rim).fillCircle(16, 16, 11);
    g.fillStyle(c).fillCircle(16, 16, 8);
    g.fillStyle(palette.bone).fillCircle(13, 13, 2);
  };
}

function pelt(c: number): Draw {
  return (g) => {
    g.fillStyle(c)
      .fillRoundedRect(6, 8, 20, 18, 5)
      .fillTriangle(6, 26, 11, 26, 8, 30)
      .fillTriangle(21, 26, 26, 26, 24, 30);
    g.lineStyle(1, palette.dark).strokeRoundedRect(6, 8, 20, 18, 5);
  };
}

function scale(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillTriangle(16, 4, 28, 16, 16, 28).fillTriangle(16, 4, 4, 16, 16, 28);
    g.lineStyle(1, palette.string).lineBetween(16, 6, 16, 26);
  };
}

function crystal(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillTriangle(16, 2, 22, 16, 10, 16).fillTriangle(10, 16, 22, 16, 16, 30);
    g.fillStyle(palette.string).fillTriangle(16, 2, 19, 16, 13, 16);
  };
}

function wing(c: number): Draw {
  return (g) => {
    g.fillStyle(c).fillTriangle(4, 8, 28, 4, 22, 28).fillTriangle(4, 8, 22, 28, 8, 20);
  };
}

type EtcItem = Extract<(typeof items)[number], { tab: 'etc' }>['id'];

const materials = {
  slime_gel: gel(palette.gel),
  mushroom_cap: (g) => {
    g.fillStyle(palette.orange).fillEllipse(16, 16, 26, 18);
    g.fillStyle(palette.bone).fillCircle(10, 14, 3).fillCircle(20, 12, 3);
  },
  boar_tusk: (g) => {
    g.fillStyle(palette.bone)
      .fillTriangle(6, 26, 14, 4, 26, 10)
      .fillTriangle(6, 26, 26, 10, 14, 14);
  },
  bat_wing: wing(palette.dark),
  spore_jelly: gel(0xb07ad0),
  moss_shell: shell(0x6aa84f),
  moth_dust: dust(0xd8c8a0),
  spotted_tail: tail(0xc04040),
  glow_gel: gel(0xe0e070),
  elder_shell: shell(0x8a6ad0),
  sand_whisker: whiskers(0xe0c080),
  scarab_shell: shell(0x4a8ab0),
  vulture_feather: feather(0xa07050),
  sun_gel: gel(0xf0a030),
  tomb_fang: fang(0xd8cfa0),
  wisp_essence: orb(0x60e0d0, 0x2a7a70),
  frost_gel: gel(0xa0d8f0),
  snow_pelt: pelt(0xf0f0f0),
  ice_scale: scale(0x80b0f0),
  glacier_shell: shell(0x9ad0e0),
  ice_crystal: crystal(0xd0e8ff),
  rime_fang: fang(0xb8d8f8),
  shade_wing: wing(0x6a4a8a),
  gloom_core: orb(palette.dark, 0x6a4a8a),
} satisfies Record<string, Draw>;

// An etc item the data has but this table lacks fails the type check.
const _materials: Record<EtcItem, Draw> = materials;
void _materials;

const drawnMaterials: Readonly<Record<string, Draw>> = materials;

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
    return drawnMaterials[id] ?? unknown;
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
