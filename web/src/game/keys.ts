import { items } from '@/gamedata/items';
import { maps } from '@/gamedata/maps';

export const SCENES = {
  Boot: 'Boot',
  Preloader: 'Preloader',
  World: 'World',
} as const;

export const TEX = {
  dirtMid: 'dirtMid',
  dirtCenter: 'dirtCenter',
  dirtHalf: 'dirtHalf',
  grassMid: 'grassMid',
  grassCenter: 'grassCenter',
  grassHalf: 'grassHalf',
  stoneMid: 'stoneMid',
  stoneCenter: 'stoneCenter',
  stoneHalf: 'stoneHalf',
  ladderMid: 'ladderMid',
  ladderTop: 'ladderTop',
  doorClosedMid: 'doorClosedMid',
  doorClosedTop: 'doorClosedTop',
  doorOpenMid: 'doorOpenMid',
  doorOpenTop: 'doorOpenTop',
  bgGrass: 'bgGrass',
  bgLand: 'bgLand',
  bgShroom: 'bgShroom',
  bgCave: 'bgCave',
  warriorStand: 'warriorStand',
  warriorWalk1: 'warriorWalk1',
  warriorWalk2: 'warriorWalk2',
  warriorJump: 'warriorJump',
  warriorClimb1: 'warriorClimb1',
  warriorClimb2: 'warriorClimb2',
  warriorHit: 'warriorHit',
  bowmanStand: 'bowmanStand',
  bowmanWalk1: 'bowmanWalk1',
  bowmanWalk2: 'bowmanWalk2',
  bowmanJump: 'bowmanJump',
  bowmanClimb1: 'bowmanClimb1',
  bowmanClimb2: 'bowmanClimb2',
  bowmanHit: 'bowmanHit',
  slimeStand: 'slimeStand',
  slimeMove: 'slimeMove',
  slimeHit: 'slimeHit',
  slimeDead: 'slimeDead',
  snailStand: 'snailStand',
  snailMove: 'snailMove',
  snailDead: 'snailDead',
  mouseStand: 'mouseStand',
  mouseMove: 'mouseMove',
  mouseDead: 'mouseDead',
  flyStand: 'flyStand',
  flyMove: 'flyMove',
  flyDead: 'flyDead',
  flask: 'flask',
} as const;
export type TextureKey = (typeof TEX)[keyof typeof TEX];

/** Drawn from the flask texture with a tint. */
export const ITEM_TINTS: Readonly<Record<string, number>> = {
  red_potion: 0xe04848,
  blue_potion: 0x4c8be0,
  orange_potion: 0xf08a3a,
  white_potion: 0xf4f4f4,
  mana_elixir: 0x9a6ae0,
};

/** Every other template in the catalog gets a texture drawn at runtime under its id. */
export const DRAWN_ITEMS: readonly string[] = items
  .map((i) => i.id)
  .filter((id) => !(id in ITEM_TINTS));

/** Drawn for a template this client does not know, as from a newer server. */
export const UNKNOWN_ITEM = 'item:unknown';

export function itemTexture(template: string): string {
  if (template in ITEM_TINTS) {
    return TEX.flask;
  }
  return DRAWN_ITEMS.includes(template) ? template : UNKNOWN_ITEM;
}

export const MAP_IDS = maps.map((m) => m.id);
export const mapKey = (id: string): string => `map:${id}`;

export const ANIM = {
  warriorWalk: 'warrior:walk',
  warriorClimb: 'warrior:climb',
  bowmanWalk: 'bowman:walk',
  bowmanClimb: 'bowman:climb',
  slimeMove: 'slime:move',
  snailMove: 'snail:move',
  mouseMove: 'mouse:move',
  flyMove: 'fly:move',
} as const;

export interface Anim {
  readonly key: string;
  readonly frames: readonly string[];
  readonly frameRate: number;
}

/** The looping animations, each over its frames' textures. */
export const animations: readonly Anim[] = [
  { key: ANIM.warriorWalk, frames: [TEX.warriorWalk1, TEX.warriorWalk2], frameRate: 8 },
  { key: ANIM.warriorClimb, frames: [TEX.warriorClimb1, TEX.warriorClimb2], frameRate: 6 },
  { key: ANIM.bowmanWalk, frames: [TEX.bowmanWalk1, TEX.bowmanWalk2], frameRate: 8 },
  { key: ANIM.bowmanClimb, frames: [TEX.bowmanClimb1, TEX.bowmanClimb2], frameRate: 6 },
  { key: ANIM.slimeMove, frames: [TEX.slimeStand, TEX.slimeMove], frameRate: 4 },
  { key: ANIM.snailMove, frames: [TEX.snailStand, TEX.snailMove], frameRate: 3 },
  { key: ANIM.mouseMove, frames: [TEX.mouseStand, TEX.mouseMove], frameRate: 8 },
  { key: ANIM.flyMove, frames: [TEX.flyStand, TEX.flyMove], frameRate: 10 },
];

/** The grey copy of a texture or animation, which a tint colours fully. */
export const greyKey = (key: string): string => `${key}:grey`;

/** 128 px art scaled to the 32 px grid. */
export const ART_SCALE = 0.25;
export const TILE = 32;
