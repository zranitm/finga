// Each body's look (3d-world.md section 11): a class's, mob's or NPC's model from its look key, its tint on one part.
import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
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

const unknown = new Set<string>();

/** Says once that what, an id the server sent, isn't in this client's data, as when the client is older than the server's, and what's drawn instead. */
function stale(what: string, instead: string): void {
  if (!unknown.has(what)) {
    unknown.add(what);
    console.info(`finga: no ${what} in this client's data; drawn as ${instead}`);
  }
}

/** Piece id's look key, or none for a piece this client's data lacks: the class's own drawn for that slot. */
function pieceLook(id: string): string[] {
  const piece = outfits.pieces.find((p) => p.id === id);
  if (piece) {
    return [piece.look];
  }
  stale(`piece ${id}`, "the class's own");
  return [];
}

/** A player's model: their class's, in what their look wears; a class this client's data lacks is drawn as the first, the Beginner, as its attacks are. */
export function playerLook(cls: string, look: LookWire | undefined): ModelLook {
  const known = classes.some((c) => c.id === cls) ? cls : classes[0].id;
  if (known !== cls) {
    stale(`class ${cls}`, `a ${classes[0].name}`);
  }
  if (!look || drawnAs(known, look) === known) {
    return classLook(known);
  }
  const looks = slotsOf(look)
    .filter((id) => id !== '')
    .flatMap(pieceLook);
  return classLook(known, { looks, bareHead: look.hideHeadwear });
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
      throw new Error(`no piece look ${k}`); // the generated data names a look the client hasn't mapped, which looks.test refuses
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

/** The mob drawn for a type this client's data lacks: the smallest, so a stand-in never looms. */
const STAND_IN = mobs.reduce((a, b) => (b.radius < a.radius ? b : a));

/** A mob's model at its cylinder's height, which its look's scale is already part of, its tint on the main material, drawn no wider than its circle; a type this client's data lacks is drawn as the smallest mob. */
export function mobLook(type: string): ModelLook {
  let m = mobById(type);
  if (!m) {
    stale(`mob ${type}`, `a ${STAND_IN.name}`);
    m = STAND_IN;
  }
  return { ...modelLook(m.look.model, m.height, m.look.tint), width: 2 * m.radius };
}
