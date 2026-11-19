// Each body's look (3d-world.md section 11): a class's, mob's or NPC's model from its look key, its tint on one part.
import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';
import { outfits } from '@/gamedata/outfits';
import { mobById } from '@/progression/mobs';

import type { LookWire } from '../systems/interpolation';

import { MODELS } from './model-looks';
import type { Model } from './model-looks';
import { PIECE_LOOKS } from './outfit-looks';
import type { PieceLook } from './outfit-looks';

export type { ActionClip, Clips } from './model-looks';

/** A colour, sRGB, drawn in place of one material's colour or over one mesh's texture. */
export interface Tint {
  readonly color: number;
  readonly part: string;
}

/** A model key's look at its size: the height it stands, the circle a mob's fits in, and its tint. */
export interface ModelLook extends Omit<Model, 'tinted'> {
  readonly height: number; // metres the model stands
  readonly width?: number; // the most metres it may be across: a mob's circle, which it's drawn inside
  readonly tint?: Tint;
  readonly wear?: readonly PieceLook[]; // a character's outfit pieces, drawn over its class's own
  readonly bareHead?: boolean; // its class's headwear hidden, and none worn
}

/** What a character wears over its class's look: its pieces' look keys, and whether it shows no headwear at all. */
export interface Wearing {
  readonly looks: readonly string[];
  readonly bareHead: boolean;
}

const slotsOf = (look: LookWire) => [
  look.outfit,
  look.headwear,
  look.face,
  look.weapon,
  look.wings,
];

/** What a body is drawn as, its class and what its look shows, so a change of either redraws it; a look showing nothing is the class alone. */
export function drawnAs(cls: string, look: LookWire | undefined): string {
  const shown = look ? [...slotsOf(look), look.hideHeadwear ? 'bare' : ''] : [];
  return shown.every((s) => s === '') ? cls : `${cls} ${shown.join('|')}`;
}

/** A player's model: their class's, in what their look wears. */
export function playerLook(cls: string, look: LookWire | undefined): ModelLook {
  if (!look || drawnAs(cls, look) === cls) {
    return classLook(cls);
  }
  const looks = slotsOf(look)
    .filter((id) => id !== '')
    .map((id) => {
      const piece = outfits.pieces.find((p) => p.id === id);
      if (!piece) {
        throw new Error(`no piece ${id}`); // the server wears only pieces its data has, which this was built from
      }
      return piece.look;
    });
  return classLook(cls, { looks, bareHead: look.hideHeadwear });
}

function modelLook(key: string, height: number, tint: number | null): ModelLook {
  const model = MODELS[key];
  if (!model) {
    throw new Error(`no model ${key}`); // the data names a model the client hasn't mapped
  }
  const { tinted, ...m } = model;
  return {
    ...m,
    height,
    ...(tint !== null && tinted !== undefined && { tint: { color: tint, part: tinted } }),
  };
}

/** A class's model, at the character's height, its tint on its cape, and what the character wears over it. */
export function classLook(cls: string, wearing?: Wearing): ModelLook {
  const c = classes.find((k) => k.id === cls);
  if (!c) {
    throw new Error(`no class ${cls}`);
  }
  const look = modelLook(c.look.model, movement.height, c.look.tint);
  if (!wearing) {
    return look;
  }
  const wear = wearing.looks.map((k) => {
    const piece = PIECE_LOOKS[k];
    if (!piece) {
      throw new Error(`no piece look ${k}`); // the data names a look the client hasn't mapped
    }
    return piece;
  });
  return wearing.bareHead
    ? { ...look, wear: wear.filter((p) => p.slot !== 'headwear'), bareHead: true }
    : { ...look, wear };
}

/** An NPC's model by its look key: a trainer as its class's, the guide as the Barbarian dressed. */
export function npcLook(look: string): ModelLook {
  const trainer = /^trainer_(.+)$/.exec(look)?.[1];
  return trainer ? classLook(trainer) : modelLook(look, movement.height, null);
}

/** A mob's model at its cylinder's height, which its look's scale is already part of, its tint on the main material, drawn no wider than its circle. */
export function mobLook(type: string): ModelLook {
  const m = mobById(type);
  if (!m) {
    throw new Error(`no mob ${type}`);
  }
  return { ...modelLook(m.look.model, m.height, m.look.tint), width: 2 * m.radius };
}
