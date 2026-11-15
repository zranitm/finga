// Each body's look (3d-world.md section 11): a class's, mob's or NPC's model from its look key, its tint on one part, or a stand-in where no model is mapped yet.
import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';

/** An action's clip and where in it the swing is fastest, as a share of its length: that frame lands on the action's hit. */
export interface ActionClip {
  readonly clip: string;
  readonly strike: number;
}

export interface Clips {
  readonly idle: string;
  readonly walk: string;
  readonly run: string;
  readonly fall: string;
  readonly hit: string;
  readonly death: string;
  readonly attack?: ActionClip;
  readonly skill?: ActionClip;
}

/** A colour, sRGB, drawn in place of one material's colour or over one mesh's texture. */
export interface Tint {
  readonly color: number;
  readonly part: string;
}

export interface ModelLook {
  readonly file: string; // under /models
  readonly hide: readonly string[]; // the pack's other gear on the same rig
  readonly height: number; // metres the model stands
  readonly exposure: number; // the light its flat colours were authored under, as a multiple of white
  readonly tint?: Tint;
  readonly spine?: string; // the joint the upper body hangs from; without one an action plays on the whole body
  readonly weapon?: string; // the mesh a swing trails from
  readonly clips: Clips;
}

/** A mob's body with no model yet: primitives of its shape, its circle and height, in its tint. */
export interface StandInLook {
  readonly shape: StandInShape;
  readonly color: number;
  readonly radius: number;
  readonly height: number;
}

export type StandInShape = 'blob' | 'flyer' | 'shell' | 'beast' | 'brute';

export type Look = { readonly model: ModelLook } | { readonly standIn: StandInLook };

// Strikes measured on the clips: the frame the weapon hand moves fastest.
const KAYKIT = {
  idle: 'Idle',
  walk: 'Walking_A',
  run: 'Running_A',
  fall: 'Jump_Idle',
  hit: 'Hit_A',
  death: 'Death_A',
} as const;
const SLICE: ActionClip = { clip: '1H_Melee_Attack_Slice_Diagonal', strike: 0.38 };
const CHOP: ActionClip = { clip: '1H_Melee_Attack_Chop', strike: 0.54 };
const STAB: ActionClip = { clip: '1H_Melee_Attack_Stab', strike: 0.24 };
const SHOOT: ActionClip = { clip: '2H_Ranged_Shoot', strike: 0.14 };
const CAST: ActionClip = { clip: 'Spellcast_Shoot', strike: 0.09 };
const RAISE: ActionClip = { clip: 'Spellcast_Raise', strike: 0.13 };

const QUATERNIUS = {
  idle: 'CharacterArmature|Idle',
  walk: 'CharacterArmature|Walk',
  run: 'CharacterArmature|Walk',
  fall: 'CharacterArmature|Jump',
  hit: 'CharacterArmature|HitRecieve',
  death: 'CharacterArmature|Death',
  attack: { clip: 'CharacterArmature|Bite_Front', strike: 0.42 },
} as const;

/** Quaternius's eye white is 0.28 linear: lit 3.6 times over it reads white, as the pack's own renders show. */
const QUATERNIUS_EXPOSURE = 3.6;

type Model = Omit<ModelLook, 'height' | 'tint'> & { readonly tinted?: string };

const BARBARIAN_GEAR = ['Barbarian_Round_Shield', '1H_Axe_Offhand', '2H_Axe', 'Mug'];

/** Each model key's file, the gear it hides, its clips and the part a tint colours. */
const MODELS: Readonly<Record<string, Model>> = {
  // The Beginner: the Barbarian without hat or cape, its one-handed axe standing in for the club.
  barbarian: {
    file: 'kaykit/Barbarian.glb',
    hide: [...BARBARIAN_GEAR, 'Barbarian_Hat', 'Barbarian_Cape'],
    exposure: 1,
    spine: 'spine',
    weapon: '1H_Axe',
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP },
  },
  // The town guide: the Barbarian with hat and cape, so it doesn't pass for a Beginner.
  guide: {
    file: 'kaykit/Barbarian.glb',
    hide: BARBARIAN_GEAR,
    exposure: 1,
    spine: 'spine',
    clips: KAYKIT,
  },
  knight: {
    file: 'kaykit/Knight.glb',
    hide: [
      '1H_Sword_Offhand',
      'Badge_Shield',
      'Rectangle_Shield',
      'Round_Shield',
      'Spike_Shield',
      '2H_Sword',
    ],
    exposure: 1,
    spine: 'spine',
    weapon: '1H_Sword',
    tinted: 'Knight_Cape',
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP },
  },
  rogue: {
    file: 'kaykit/Rogue.glb',
    hide: ['Knife_Offhand', '1H_Crossbow', '2H_Crossbow', 'Throwable'],
    exposure: 1,
    spine: 'spine',
    weapon: 'Knife',
    tinted: 'Rogue_Cape',
    clips: { ...KAYKIT, attack: STAB, skill: CHOP },
  },
  rogue_hooded: {
    file: 'kaykit/Rogue_Hooded.glb',
    hide: ['Knife', 'Knife_Offhand', '1H_Crossbow', 'Throwable'],
    exposure: 1,
    spine: 'spine',
    tinted: 'Rogue_Cape',
    clips: { ...KAYKIT, attack: SHOOT, skill: SHOOT },
  },
  mage: {
    file: 'kaykit/Mage.glb',
    hide: ['Spellbook', 'Spellbook_open', '1H_Wand'],
    exposure: 1,
    spine: 'spine',
    tinted: 'Mage_Cape',
    clips: { ...KAYKIT, attack: CAST, skill: RAISE },
  },
  green_blob: {
    file: 'quaternius/GreenBlob.glb',
    hide: [],
    exposure: QUATERNIUS_EXPOSURE,
    tinted: 'Green_Main',
    clips: QUATERNIUS,
  },
  mushnub: {
    file: 'quaternius/Mushnub.glb',
    hide: [],
    exposure: QUATERNIUS_EXPOSURE,
    tinted: 'MushroomKing_Secondary',
    clips: QUATERNIUS,
  },
};

function modelLook(key: string, height: number, tint: number | null): ModelLook {
  const { tinted, ...m } = MODELS[key] ?? MODELS.barbarian!;
  return {
    ...m,
    height,
    ...(tint !== null && tinted !== undefined && { tint: { color: tint, part: tinted } }),
  };
}

/** A class's model, at the character's height, its tint on its cape. */
export function classLook(cls: string): ModelLook {
  const c = classes.find((k) => k.id === cls) ?? classes[0];
  return modelLook(c.look.model, movement.height, c.look.tint);
}

/** An NPC's model: a trainer as its class's, anyone else as the guide. */
export function npcLook(look: string): ModelLook {
  const trainer = /^trainer_(.+)$/.exec(look)?.[1];
  return trainer ? classLook(trainer) : modelLook('guide', movement.height, null);
}

/** A mob's model at its cylinder's height, which its look's scale is already part of, its tint on the main material; a stand-in when it has no model. */
export function mobLook(type: string): Look {
  const m = mobs.find((k) => k.id === type);
  if (!m) {
    return { standIn: { shape: 'blob', color: 0x9a9a9a, radius: 0.5, height: 1 } };
  }
  if (m.look.model !== null && MODELS[m.look.model]) {
    return { model: modelLook(m.look.model, m.height, m.look.tint) };
  }
  return {
    standIn: {
      shape: standInShape(m.id),
      color: m.look.tint ?? 0x9a9a9a,
      radius: m.radius,
      height: m.height,
    },
  };
}

/** The words in a mob's id that name its family's stand-in shape; any other is a brute. Slimes have a model. */
const FAMILIES: readonly (readonly [StandInShape, readonly string[]])[] = [
  ['flyer', ['bat', 'moth', 'vulture', 'wing', 'wisp']],
  ['shell', ['snail', 'scarab']],
  ['beast', ['rat', 'mouse', 'weasel', 'boar']],
];

/** The stand-in's shape by the family its id names. */
function standInShape(id: string): StandInShape {
  return FAMILIES.find(([, words]) => words.some((w) => id.includes(w)))?.[0] ?? 'brute';
}
