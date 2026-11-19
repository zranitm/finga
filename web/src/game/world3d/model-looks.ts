// Each model key's file, gear and clips (3d-world.md section 11), apart from the game data so `npm run models` reads them too.

/** An action's clip and where in it the swing is fastest, as a share of its length: that frame lands on the action's hit. */
export interface ActionClip {
  readonly clip: string;
  readonly strike: number;
}

/** The clip an action plays: a basic attack's, a skill's, or a block's guard (C21). */
export type ActionPose = 'attack' | 'skill' | 'block';

export interface Clips {
  readonly idle: string;
  readonly walk: string;
  readonly run: string;
  readonly fall: string;
  readonly hit: string;
  readonly death: string;
  readonly attack?: ActionClip;
  readonly skill?: ActionClip;
  readonly block?: ActionClip;
}

/** A model key's look before its height and tint: its file, the gear it hides, and the part a tint colours. */
export interface Model {
  readonly file: string; // under /models
  readonly hide: readonly string[]; // gear in the file another look shows
  readonly exposure: number; // the light its flat colours were authored under, as a multiple of white
  readonly spine?: string; // the joint the upper body hangs from; without one an action plays on the whole body
  readonly weapon?: string; // the mesh a swing trails from
  readonly tinted?: string; // the part a tint colours
  readonly flies?: boolean; // drawn at its height's middle, as the spits it shoots, when its circle draws it shorter
  readonly over?: readonly string[]; // meshes worn over the body, as a boss's crown, that its fit to the mob's height and circle leaves out
  readonly own?: Own; // a class's: the meshes each slot's outfit piece takes the place of
  readonly hood?: Hood;
  readonly clips: Clips;
}

/** A head with its hood built in, headwear in all but name: under a worn piece of headwear it's swapped for the bare head, from file's parts on the same rig, so only one thing sits on the head. */
export interface Hood {
  readonly head: string;
  readonly file: string;
  readonly bare: string;
}

/** A class model's own meshes for the outfit slots (outfits.md section 6): its body below the head and its cape, its headwear, its weapon. */
export interface Own {
  readonly outfit: readonly string[];
  readonly headwear: readonly string[];
  readonly weapon: readonly string[];
}

const ownBody = (who: string) => [
  `${who}_ArmLeft`,
  `${who}_ArmRight`,
  `${who}_Body`,
  `${who}_LegLeft`,
  `${who}_LegRight`,
  `${who}_Cape`,
];

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
const GUARD: ActionClip = { clip: 'Block', strike: 0.28 }; // the guard is up by 0.28 and held to the end

const QUATERNIUS = {
  idle: 'CharacterArmature|Idle',
  walk: 'CharacterArmature|Walk',
  run: 'CharacterArmature|Walk',
  fall: 'CharacterArmature|Jump',
  hit: 'CharacterArmature|HitRecieve',
  death: 'CharacterArmature|Death',
  attack: { clip: 'CharacterArmature|Bite_Front', strike: 0.42 },
} as const;

// Ultimate Monsters as converted here: one flat-colour material per atlas colour, its clips by plain name, a rig per group. Strikes measured on the clips: the frame the head or the knuckles move fastest.
const MONSTER_BLOB = {
  idle: 'Idle',
  walk: 'Walk',
  run: 'Walk',
  fall: 'Jump',
  hit: 'HitRecieve',
  death: 'Death',
  attack: { clip: 'Bite_Front', strike: 0.42 },
} as const;
const MONSTER_BIG = {
  idle: 'Idle',
  walk: 'Walk',
  run: 'Run',
  fall: 'Jump_Idle',
  hit: 'HitReact',
  death: 'Death',
  attack: { clip: 'Punch', strike: 0.38 },
} as const;
const MONSTER_FLYING = {
  idle: 'Flying_Idle',
  walk: 'Fast_Flying',
  run: 'Fast_Flying',
  fall: 'Flying_Idle',
  hit: 'HitReact',
  death: 'Death',
  attack: { clip: 'Headbutt', strike: 0.18 },
} as const;

/** Quaternius's eye white is 0.28 linear: lit 3.6 times over it reads white, as the pack's own renders show. */
const QUATERNIUS_EXPOSURE = 3.6;

/** The full pack's palette, as converted here, is each Poly Pizza colour times 1.25: lit 0.8 as much, it draws the same. */
const MONSTER_EXPOSURE = QUATERNIUS_EXPOSURE * 0.8;

/** Each model key's file, the gear it hides, its clips and the part a tint colours. */
export const MODELS: Readonly<Record<string, Model>> = {
  // The Beginner: the Barbarian without hat or cape, its one-handed axe standing in for the club.
  barbarian: {
    file: 'kaykit/Barbarian.glb',
    hide: ['Barbarian_Hat', 'Barbarian_Cape'],
    exposure: 1,
    spine: 'spine',
    weapon: '1H_Axe',
    own: { outfit: ownBody('Barbarian'), headwear: ['Barbarian_Hat'], weapon: ['1H_Axe'] },
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP, block: GUARD },
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
    own: { outfit: ownBody('Knight'), headwear: ['Knight_Helmet'], weapon: ['1H_Sword'] },
    clips: { ...KAYKIT, attack: SLICE, skill: CHOP, block: GUARD },
  },
  rogue: {
    file: 'kaykit/Rogue.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    weapon: 'Knife',
    tinted: 'Rogue_Cape',
    own: { outfit: ownBody('Rogue'), headwear: [], weapon: ['Knife'] },
    clips: { ...KAYKIT, attack: STAB, skill: CHOP, block: GUARD },
  },
  rogue_hooded: {
    file: 'kaykit/Rogue_Hooded.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    tinted: 'Rogue_Cape',
    own: { outfit: ownBody('Rogue'), headwear: [], weapon: ['2H_Crossbow'] }, // a bare head keeps the hood, which is its head
    hood: { head: 'Rogue_Head_Hooded', file: 'kaykit/Rogue.glb', bare: 'Rogue_Head' },
    clips: { ...KAYKIT, attack: SHOOT, skill: SHOOT, block: GUARD },
  },
  mage: {
    file: 'kaykit/Mage.glb',
    hide: [],
    exposure: 1,
    spine: 'spine',
    tinted: 'Mage_Cape',
    own: { outfit: ownBody('Mage'), headwear: ['Mage_Hat'], weapon: ['2H_Staff'] },
    clips: { ...KAYKIT, attack: CAST, skill: RAISE, block: GUARD },
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
  dog_blob: {
    file: 'quaternius/Blob_Dog.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  cat_blob: {
    file: 'quaternius/Blob_Cat.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  fish_blob: {
    file: 'quaternius/Blob_Fish.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  spiky_blob: {
    file: 'quaternius/Blob_GreenSpikyBlob.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  crowned_blob: {
    file: 'quaternius/GreenBlob_Crown.glb',
    hide: [],
    exposure: QUATERNIUS_EXPOSURE,
    tinted: 'Green_Main',
    over: ['Crown', 'Crown_Gem'],
    clips: QUATERNIUS,
  },
  crystal_blob: {
    file: 'quaternius/GreenBlob_Crystals.glb',
    hide: [],
    exposure: QUATERNIUS_EXPOSURE,
    tinted: 'Green_Main',
    over: ['Crystals', 'Crystals_Deep'],
    clips: QUATERNIUS,
  },
  mushnub_evolved: {
    file: 'quaternius/Blob_Mushnub_Evolved.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  pink_blob: {
    file: 'quaternius/Blob_PinkBlob.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  yeti_blob: {
    file: 'quaternius/Blob_Yeti.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BLOB,
  },
  monkroose: {
    file: 'quaternius/Big_Monkroose.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BIG,
  },
  bunny: {
    file: 'quaternius/Big_Bunny.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BIG,
  },
  yeti: {
    file: 'quaternius/Big_Yeti.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BIG,
  },
  cactoro: {
    file: 'quaternius/Big_Cactoro.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BIG,
  },
  orc_skull: {
    file: 'quaternius/Big_Orc_Skull.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    clips: MONSTER_BIG,
  },
  bat: {
    file: 'quaternius/Flying_Goleling.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  bat_king: {
    file: 'quaternius/Flying_Goleling_Evolved.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  bee: {
    file: 'quaternius/Flying_Armabee.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  bee_queen: {
    file: 'quaternius/Flying_Armabee_Evolved.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  pigeon: {
    file: 'quaternius/Flying_Pigeon.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  squidle: {
    file: 'quaternius/Flying_Squidle.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  ghost: {
    file: 'quaternius/Flying_Ghost.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  ghost_skull: {
    file: 'quaternius/Flying_Ghost_Skull.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  dragon: {
    file: 'quaternius/Flying_Dragon.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: MONSTER_FLYING,
  },
  dragon_king: {
    file: 'quaternius/Flying_Dragon_Evolved.glb',
    hide: [],
    exposure: MONSTER_EXPOSURE,
    tinted: 'Main',
    flies: true,
    clips: { ...MONSTER_FLYING, attack: { clip: 'Headbutt', strike: 0.14 } },
  },
};

/** The clips a look plays, each once. */
export function clipsOf(look: { readonly clips: Clips }): string[] {
  const c = look.clips;
  const all = [
    c.idle,
    c.walk,
    c.run,
    c.fall,
    c.hit,
    c.death,
    c.attack?.clip,
    c.skill?.clip,
    c.block?.clip,
  ];
  return [...new Set(all.filter((x): x is string => x !== undefined))];
}
