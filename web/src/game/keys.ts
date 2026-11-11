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
export type MapId = (typeof maps)[number]['id'];
export function isMapId(id: string): id is MapId {
  return (MAP_IDS as readonly string[]).includes(id);
}
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

/** 128 px art scaled to the 32 px grid. */
export const ART_SCALE = 0.25;
export const TILE = 32;
