// The spike's models, clips and tints, decided 2026-11-14; the slice moves them into classes.yaml and mobs.yaml as look keys.
import { mobs } from '@/gamedata/mobs';

/** 3d-world.md section 3: 1 m is 32 px. */
const PX_PER_M = 32;
/** The character cylinder's height, from 56 px. */
const CHARACTER_HEIGHT = 1.75;

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
  readonly attack?: ActionClip;
  readonly skill?: ActionClip;
}

export interface ModelLook {
  readonly file: string; // under /models
  readonly hide: readonly string[]; // the pack's other gear on the same rig
  readonly height: number; // metres the model stands
  readonly exposure: number; // the light its flat colours were authored under, as a multiple of white
  readonly tint?: { readonly color: number; readonly material: string }; // sRGB, drawn in place of that material's colour
  readonly spine?: string; // the joint the upper body hangs from; without one an action plays on the whole body
  readonly weapon?: string; // the mesh a swing trails from
  readonly clips: Clips;
}

// Strikes measured on the clips: the frame the weapon hand moves fastest.
const KAYKIT_CLIPS: Clips = {
  idle: 'Idle',
  walk: 'Walking_A',
  run: 'Running_A',
  fall: 'Jump_Idle',
  attack: { clip: '1H_Melee_Attack_Slice_Diagonal', strike: 0.38 },
  skill: { clip: '1H_Melee_Attack_Chop', strike: 0.54 },
};

const QUATERNIUS_CLIPS: Clips = {
  idle: 'CharacterArmature|Idle',
  walk: 'CharacterArmature|Walk',
  run: 'CharacterArmature|Walk',
  fall: 'CharacterArmature|Jump',
  attack: { clip: 'CharacterArmature|Bite_Front', strike: 0.42 },
};

/** Quaternius's eye white is 0.28 linear: lit 3.6 times over it reads white, as the pack's own renders show. */
const QUATERNIUS_EXPOSURE = 3.6;

/** The Beginner is the KayKit Barbarian without hat, cape or shield, its one-handed axe standing in for the club. */
const beginner: ModelLook = {
  file: 'kaykit/Barbarian.glb',
  hide: [
    'Barbarian_Hat',
    'Barbarian_Cape',
    'Barbarian_Round_Shield',
    '1H_Axe_Offhand',
    '2H_Axe',
    'Mug',
  ],
  height: CHARACTER_HEIGHT,
  exposure: 1,
  spine: 'spine',
  weapon: '1H_Axe',
  clips: KAYKIT_CLIPS,
};

/** The Warrior is the KayKit Knight with its one-handed sword alone: the game has no shields. */
const warrior: ModelLook = {
  file: 'kaykit/Knight.glb',
  hide: [
    '1H_Sword_Offhand',
    'Badge_Shield',
    'Rectangle_Shield',
    'Round_Shield',
    'Spike_Shield',
    '2H_Sword',
  ],
  height: CHARACTER_HEIGHT,
  exposure: 1,
  spine: 'spine',
  weapon: '1H_Sword',
  clips: KAYKIT_CLIPS,
};

/** The spike carries the Beginner's and the Warrior's models; the other classes draw as the Beginner until the slice. */
export function classLook(cls: string): ModelLook {
  return cls === 'warrior' ? warrior : beginner;
}

/** A mob's look, with the radius of its circle in metres: half its width. */
export interface MobLook extends ModelLook {
  readonly id: string;
  readonly radius: number;
}

const mob = (id: string, file: string, tint?: ModelLook['tint']): MobLook => {
  const size = mobs.find((m) => m.id === id);
  return {
    id,
    file: `quaternius/${file}`,
    hide: [],
    height: (size?.height ?? PX_PER_M) / PX_PER_M,
    radius: (size?.width ?? PX_PER_M) / 2 / PX_PER_M,
    exposure: QUATERNIUS_EXPOSURE,
    ...(tint ? { tint } : {}),
    clips: QUATERNIUS_CLIPS,
  };
};

/** The forest's mobs: Quaternius's Green Blob and Mushnub with an orange cap; the king slime is the blob three times over, darker. */
export const forestMobs: readonly MobLook[] = [
  mob('green_slime', 'GreenBlob.glb'),
  mob('orange_mushroom', 'Mushnub.glb', { color: 0xe08a3c, material: 'MushroomKing_Secondary' }),
  mob('king_slime', 'GreenBlob.glb', { color: 0x4a6b3c, material: 'Green_Main' }),
];
