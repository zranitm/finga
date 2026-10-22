// Scene, texture and animation keys as constants (page 7, Phaser
// conventions): a mistyped string key is a common Phaser bug.
export const SCENES = {
  Boot: 'Boot',
  Preloader: 'Preloader',
  World: 'World',
} as const;

/** Textures loaded from public/assets (see game/assets.ts for the files). */
export const TEX = {
  // ground styles: one look per map (town, forest, cave)
  dirtMid: 'dirtMid',
  dirtCenter: 'dirtCenter',
  dirtHalf: 'dirtHalf',
  grassMid: 'grassMid',
  grassCenter: 'grassCenter',
  grassHalf: 'grassHalf',
  stoneMid: 'stoneMid',
  stoneCenter: 'stoneCenter',
  stoneHalf: 'stoneHalf',
  // ropes and portals
  ladderMid: 'ladderMid',
  ladderTop: 'ladderTop',
  doorClosedMid: 'doorClosedMid',
  doorClosedTop: 'doorClosedTop',
  doorOpenMid: 'doorOpenMid',
  doorOpenTop: 'doorOpenTop',
  // backgrounds
  bgGrass: 'bgGrass',
  bgLand: 'bgLand',
  bgShroom: 'bgShroom',
  bgCave: 'bgCave',
  // players: <class><Pose>
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
  // mobs: <mob><Pose>
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
  // items
  flask: 'flask',
} as const;
export type TextureKey = (typeof TEX)[keyof typeof TEX];

/** Item textures drawn at runtime (LICENSES.md, original placeholders), keyed by template id. */
export const ITEM_PLACEHOLDERS = [
  'wooden_sword',
  'short_bow',
  'iron_sword',
  'hunting_bow',
  'leather_cap',
  'leather_vest',
  'slime_gel',
  'mushroom_cap',
  'boar_tusk',
  'bat_wing',
] as const;

/** Item templates drawn from the flask texture with a tint. */
export const ITEM_TINTS: Readonly<Record<string, number>> = {
  red_potion: 0xe04848,
  blue_potion: 0x4c8be0,
};

/** The texture an item template is drawn with: the flask for potions, else its placeholder. */
export function itemTexture(template: string): string {
  return template in ITEM_TINTS ? TEX.flask : template;
}

export const MAP_IDS = ['town', 'forest', 'cave'] as const;
export type MapId = (typeof MAP_IDS)[number];
export function isMapId(id: string): id is MapId {
  return (MAP_IDS as readonly string[]).includes(id);
}
/** The cache key of a map's JSON. */
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

/** Phaser scales the 128 px art by this so one tile fills one 32 px grid cell. */
export const ART_SCALE = 0.25;
export const TILE = 32;
