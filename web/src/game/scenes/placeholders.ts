// Item templates with no Kenney match get a 32 px shape drawn at runtime
// (public/assets/LICENSES.md, "Original placeholders"). Palette picked from
// the Kenney art so they sit beside it.
import type Phaser from 'phaser';

import { ITEM_PLACEHOLDERS, TILE } from '../keys';

const palette = {
  wood: 0x8a5a3c,
  iron: 0xa0a0a8,
  leather: 0xa8744a,
  gel: 0x7ac74f,
  orange: 0xe08a3c,
  bone: 0xf0ead6,
  dark: 0x3a3a4a,
  string: 0xe6e6e6,
} as const;

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

const draws: Readonly<Record<(typeof ITEM_PLACEHOLDERS)[number], Draw>> = {
  wooden_sword: sword(palette.wood),
  iron_sword: sword(palette.iron),
  short_bow: bow(palette.wood),
  hunting_bow: bow(palette.leather),
  leather_cap: (g) => {
    g.fillStyle(palette.leather).fillEllipse(16, 18, 24, 18).fillRect(4, 18, 24, 6);
  },
  leather_vest: (g) => {
    g.fillStyle(palette.leather)
      .fillTriangle(6, 6, 26, 6, 24, 28)
      .fillTriangle(6, 6, 24, 28, 8, 28);
    g.fillStyle(palette.dark).fillRect(15, 8, 2, 20);
  },
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

/** Draws every placeholder into the scene's texture manager, once. */
export function drawPlaceholders(scene: Phaser.Scene): void {
  for (const template of ITEM_PLACEHOLDERS) {
    if (scene.textures.exists(template)) {
      continue;
    }
    const g = scene.make.graphics({}, false);
    draws[template](g);
    g.generateTexture(template, TILE, TILE);
    g.destroy();
  }
}
