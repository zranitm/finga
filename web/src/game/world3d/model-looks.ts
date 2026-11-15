// Each model key's file, gear and clips (3d-world.md section 11), apart from the game data so `npm run models` reads them too.

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

/** A model key's look before its height and tint: its file, the gear it hides, and the part a tint colours. */
export interface Model {
  readonly file: string; // under /models
  readonly hide: readonly string[]; // gear in the file another look shows
  readonly exposure: number; // the light its flat colours were authored under, as a multiple of white
  readonly spine?: string; // the joint the upper body hangs from; without one an action plays on the whole body
  readonly weapon?: string; // the mesh a swing trails from
  readonly tinted?: string; // the part a tint colours
  readonly clips: Clips;
}

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

/** Each model key's file, the gear it hides, its clips and the part a tint colours. */
export const MODELS: Readonly<Record<string, Model>> = {
  // The Beginner: the Barbarian without hat or cape, its one-handed axe standing in for the club.
  barbarian: {
    file: 'kaykit/Barbarian.glb',
    hide: ['Barbarian_Hat', 'Barbarian_Cape'],
    exposure: 1,
    spine: 'spine',
    weapon: '1H_Axe',
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP },
  },
  // The town guide: the Barbarian with hat and cape, so it doesn't pass for a Beginner.
  guide: {
    file: 'kaykit/Barbarian.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    clips: KAYKIT,
  },
  knight: {
    file: 'kaykit/Knight.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    weapon: '1H_Sword',
    tinted: 'Knight_Cape',
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP },
  },
  rogue: {
    file: 'kaykit/Rogue.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    weapon: 'Knife',
    tinted: 'Rogue_Cape',
    clips: { ...KAYKIT, attack: STAB, skill: CHOP },
  },
  rogue_hooded: {
    file: 'kaykit/Rogue_Hooded.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    tinted: 'Rogue_Cape',
    clips: { ...KAYKIT, attack: SHOOT, skill: SHOOT },
  },
  mage: {
    file: 'kaykit/Mage.glb',
    hide: [],
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

/** The clips a look plays, each once. */
export function clipsOf(look: { readonly clips: Clips }): string[] {
  const c = look.clips;
  const all = [c.idle, c.walk, c.run, c.fall, c.hit, c.death, c.attack?.clip, c.skill?.clip];
  return [...new Set(all.filter((x): x is string => x !== undefined))];
}
