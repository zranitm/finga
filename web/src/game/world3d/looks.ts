// Each body's look (3d-world.md section 11): a class's, mob's or NPC's model from its look key, its tint on one part.
import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';
import { mobById } from '@/progression/mobs';

import { MODELS } from './model-looks';
import type { Model } from './model-looks';

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

/** A class's model, at the character's height, its tint on its cape. */
export function classLook(cls: string): ModelLook {
  const c = classes.find((k) => k.id === cls);
  if (!c) {
    throw new Error(`no class ${cls}`);
  }
  return modelLook(c.look.model, movement.height, c.look.tint);
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
