// Each outfit piece's look (outfits.md section 6): the file its meshes come from, its colourway, and its fit where it sits wrong; apart from the game data so `npm run models` reads them too.

/** The slots a look fills, as the wardrobe's are. */
export type Slot = 'outfit' | 'headwear' | 'face' | 'weapon' | 'wings';

/** Where a rigid piece sits on one body: moved, in metres of the model before its fit to the character's height, and scaled from where it hangs in its own file. */
export interface Fit {
  readonly offset: readonly [number, number, number];
  readonly scale: number;
}

export interface PieceLook {
  readonly slot: Slot;
  readonly file: string; // the source file whose parts file holds its meshes
  readonly meshes: readonly string[]; // skinned ones bound to the wearer's rig, rigid ones on the joint they hang from in the file; wings' two halves, each modelled spread flat across the back from its root, where its hinge goes
  readonly colourway?: string; // a COLOURWAYS key, whose atlas its meshes are drawn in
  readonly ownHead?: boolean; // a body that comes with its own head, which takes the place of the wearer's, and under which a hooded head is not swapped
  readonly glow?: boolean; // prestige wings', drawn in the lit band only, with a rim
  readonly fit?: Readonly<Record<string, Fit>>; // by the wearer's model file, where it sits wrong on that body
}

/** A recoloured copy of a source file's atlas: cells of KayKit's 8 × 4 grid, "column,row" from the top left, each to a new sRGB colour, its shading kept. */
export interface Colourway {
  readonly file: string;
  readonly swatches: Readonly<Record<string, number>>;
}

/** The cell of a KayKit atlas every head samples for its face; a cell sharing its colours is skin too, which no colourway changes. */
export const FACE_SWATCH = '0,0';

/** A source file's parts file: its pieces' meshes and its rig, no clips. */
export function partsFile(file: string): string {
  const slash = file.lastIndexOf('/');
  return `${file.slice(0, slash)}/parts/${file.slice(slash + 1)}`;
}

/** A colourway's atlas under the models, 128 px, as the forest's is. */
export function colourwayFile(key: string): string {
  return `kaykit/colourways/${key}.png`;
}

/** A KayKit body below the head: its limbs, its body and its cape. */
const body = (who: string) => [
  `${who}_ArmLeft`,
  `${who}_ArmRight`,
  `${who}_Body`,
  `${who}_LegLeft`,
  `${who}_LegRight`,
  `${who}_Cape`,
];

const KNIGHT = 'kaykit/Knight.glb';
const BARBARIAN = 'kaykit/Barbarian.glb';
const MAGE = 'kaykit/Mage.glb';
const ROGUE = 'kaykit/Rogue.glb';
const ROGUE_HOODED = 'kaykit/Rogue_Hooded.glb';
const SKELETON_WARRIOR = 'kaykit/Skeleton_Warrior.glb';
const SKELETON_MAGE = 'kaykit/Skeleton_Mage.glb';
const SKELETON_ROGUE = 'kaykit/Skeleton_Rogue.glb';
const SKELETON_MINION = 'kaykit/Skeleton_Minion.glb';
const SKELETON_WEAPONS = 'kaykit/Skeleton_Weapons.glb'; // the four weapons on a right-hand slot, as the Adventurers' hang

/** A skeleton body: its limbs, its body and its cloak or cape where it has one, and its own head, eyes and jaw, which take the place of the wearer's. */
const bones = (who: string, ...rest: string[]) =>
  ['ArmLeft', 'ArmRight', 'Body', 'LegLeft', 'LegRight', 'Eyes', 'Jaw', ...rest].map(
    (m) => `Skeleton_${who}_${m}`,
  );

/** The wings, made for the game by `npm run wings`: each pair's two halves, named for their side. */
const WINGS = 'wings/Wings.glb';
const pair = (kind: string) => [`${kind}_Wing_L`, `${kind}_Wing_R`];

/** The Knight's Helm over a Rogue's head, wide enough that the earring sits inside its cheek guard. */
const OVER_EARRING: Fit = { offset: [0, 0, 0], scale: 1.08 };

export const PIECE_LOOKS: Readonly<Record<string, PieceLook>> = {
  knight_body: { slot: 'outfit', file: KNIGHT, meshes: body('Knight') },
  knight_crimson: {
    slot: 'outfit',
    file: KNIGHT,
    meshes: body('Knight'),
    colourway: 'knight_crimson',
  },
  knight_platinum: {
    slot: 'outfit',
    file: KNIGHT,
    meshes: body('Knight'),
    colourway: 'knight_platinum',
  },
  barbarian_body: { slot: 'outfit', file: BARBARIAN, meshes: body('Barbarian') },
  mage_body: { slot: 'outfit', file: MAGE, meshes: body('Mage') },
  rogue_body: { slot: 'outfit', file: ROGUE, meshes: body('Rogue') },
  knight_helmet: {
    slot: 'headwear',
    file: KNIGHT,
    meshes: ['Knight_Helmet'],
    fit: { [ROGUE]: OVER_EARRING, [ROGUE_HOODED]: OVER_EARRING },
  },
  knight_helmet_silver: {
    slot: 'headwear',
    file: KNIGHT,
    meshes: ['Knight_Helmet'],
    fit: { [ROGUE]: OVER_EARRING, [ROGUE_HOODED]: OVER_EARRING },
    colourway: 'knight_silver',
  },
  knight_helmet_gold: {
    slot: 'headwear',
    file: KNIGHT,
    meshes: ['Knight_Helmet'],
    fit: { [ROGUE]: OVER_EARRING, [ROGUE_HOODED]: OVER_EARRING },
    colourway: 'knight_gold',
  },
  // Stand-ins for the feats' pieces till the catalogue is designed: looks the art has, in new colours.
  knight_helmet_crimson: {
    slot: 'headwear',
    file: KNIGHT,
    meshes: ['Knight_Helmet'],
    fit: { [ROGUE]: OVER_EARRING, [ROGUE_HOODED]: OVER_EARRING },
    colourway: 'knight_crimson',
  },
  mage_hat_green: { slot: 'headwear', file: MAGE, meshes: ['Mage_Hat'], colourway: 'mage_green' },
  knight_veteran: {
    slot: 'outfit',
    file: KNIGHT,
    meshes: body('Knight'),
    colourway: 'knight_veteran',
  },
  barbarian_hat_victor: {
    slot: 'headwear',
    file: BARBARIAN,
    meshes: ['Barbarian_Hat'],
    colourway: 'barbarian_victor',
  },
  barbarian_hat: { slot: 'headwear', file: BARBARIAN, meshes: ['Barbarian_Hat'] },
  mage_hat: { slot: 'headwear', file: MAGE, meshes: ['Mage_Hat'] },
  two_handed_sword: { slot: 'weapon', file: KNIGHT, meshes: ['2H_Sword'] },
  two_handed_axe: { slot: 'weapon', file: BARBARIAN, meshes: ['2H_Axe'] },
  wand: { slot: 'weapon', file: MAGE, meshes: ['1H_Wand'] },
  crossbow_1h: { slot: 'weapon', file: ROGUE, meshes: ['1H_Crossbow'] },
  // Stand-ins for the trials' pieces till the catalogue is designed: each class's own weapon in bronze.
  sword_bronze: { slot: 'weapon', file: KNIGHT, meshes: ['1H_Sword'], colourway: 'knight_bronze' },
  knife_bronze: { slot: 'weapon', file: ROGUE, meshes: ['Knife'], colourway: 'rogue_bronze' },
  crossbow_bronze: {
    slot: 'weapon',
    file: ROGUE,
    meshes: ['2H_Crossbow'],
    colourway: 'rogue_bronze',
  },
  staff_bronze: { slot: 'weapon', file: MAGE, meshes: ['2H_Staff'], colourway: 'mage_bronze' },
  // The Skeletons pack (Kay Lousberg, CC0), vetted for the Adventurers' 41 joints (outfits.md section 8).
  // The draft catalogue's colourways of the Adventurers' parts (outfits.md section 8), in the swatches of COLOURWAYS below.
  knight_steel: { slot: 'outfit', file: KNIGHT, meshes: body('Knight'), colourway: 'knight_steel' },
  barbarian_forest: {
    slot: 'outfit',
    file: BARBARIAN,
    meshes: body('Barbarian'),
    colourway: 'barbarian_forest',
  },
  barbarian_frost: {
    slot: 'outfit',
    file: BARBARIAN,
    meshes: body('Barbarian'),
    colourway: 'barbarian_frost',
  },
  rogue_shadow: { slot: 'outfit', file: ROGUE, meshes: body('Rogue'), colourway: 'rogue_shadow' },
  rogue_dune: { slot: 'outfit', file: ROGUE, meshes: body('Rogue'), colourway: 'rogue_dune' },
  rogue_ranger: {
    slot: 'outfit',
    file: ROGUE_HOODED,
    meshes: [...body('Rogue'), 'Rogue_Head_Hooded'], // the hood is its head, as the Bowman's own is
    colourway: 'rogue_ranger',
    ownHead: true,
  },
  mage_night: { slot: 'outfit', file: MAGE, meshes: body('Mage'), colourway: 'mage_night' },
  mage_ember: { slot: 'outfit', file: MAGE, meshes: body('Mage'), colourway: 'mage_ember' },
  mage_tide: { slot: 'outfit', file: MAGE, meshes: body('Mage'), colourway: 'mage_tide' },
  knight_helmet_black: {
    slot: 'headwear',
    file: KNIGHT,
    meshes: ['Knight_Helmet'],
    fit: { [ROGUE]: OVER_EARRING, [ROGUE_HOODED]: OVER_EARRING },
    colourway: 'knight_black',
  },
  mage_hat_night: {
    slot: 'headwear',
    file: MAGE,
    meshes: ['Mage_Hat'],
    colourway: 'mage_night_hat',
  },
  knife_shadow: { slot: 'weapon', file: ROGUE, meshes: ['Knife'], colourway: 'knife_shadow' },
  knife_gilded: { slot: 'weapon', file: ROGUE, meshes: ['Knife'], colourway: 'knife_gilded' },
  staff_crystal: { slot: 'weapon', file: MAGE, meshes: ['2H_Staff'], colourway: 'staff_crystal' },
  skeleton_warrior: {
    slot: 'outfit',
    file: SKELETON_WARRIOR,
    meshes: bones('Warrior', 'Cloak', 'Head'),
    ownHead: true,
  },
  skeleton_mage: {
    slot: 'outfit',
    file: SKELETON_MAGE,
    meshes: bones('Mage', 'Skull'),
    ownHead: true,
  },
  skeleton_rogue: {
    slot: 'outfit',
    file: SKELETON_ROGUE,
    meshes: bones('Rogue', 'Cape', 'Head'),
    ownHead: true,
  },
  skeleton_minion: {
    slot: 'outfit',
    file: SKELETON_MINION,
    meshes: bones('Minion', 'Cloak', 'Head'),
    ownHead: true,
  },
  skeleton_helmet: {
    slot: 'headwear',
    file: SKELETON_WARRIOR,
    meshes: ['Skeleton_Warrior_Helmet'],
  },
  skeleton_hat: { slot: 'headwear', file: SKELETON_MAGE, meshes: ['Skeleton_Mage_Hat'] },
  skeleton_hood: { slot: 'headwear', file: SKELETON_ROGUE, meshes: ['Skeleton_Rogue_Hood'] },
  skeleton_blade: { slot: 'weapon', file: SKELETON_WEAPONS, meshes: ['Skeleton_Blade'] },
  skeleton_axe: { slot: 'weapon', file: SKELETON_WEAPONS, meshes: ['Skeleton_Axe'] },
  skeleton_crossbow: { slot: 'weapon', file: SKELETON_WEAPONS, meshes: ['Skeleton_Crossbow'] },
  skeleton_staff: { slot: 'weapon', file: SKELETON_WEAPONS, meshes: ['Skeleton_Staff'] },
  // The catalogue's candidate wings (outfits.md section 8), looks only till the user picks: their hinge angles are the flap table's, in wear.ts.
  wings_feather: { slot: 'wings', file: WINGS, meshes: pair('Feathered') },
  wings_raven: { slot: 'wings', file: WINGS, meshes: pair('Feathered'), colourway: 'wings_raven' },
  wings_bat: { slot: 'wings', file: WINGS, meshes: pair('Bat') },
  wings_butterfly: { slot: 'wings', file: WINGS, meshes: pair('Butterfly') },
  wings_dragon: { slot: 'wings', file: WINGS, meshes: pair('Dragon'), glow: true },
  wings_crystal: { slot: 'wings', file: WINGS, meshes: pair('Crystal'), glow: true },
};

export const COLOURWAYS: Readonly<Record<string, Colourway>> = {
  knight_crimson: { file: KNIGHT, swatches: { '3,0': 0x9b2335, '7,0': 0x5e1a24, '0,1': 0x2b2b33 } }, // the plate's two steels, the tabard dark
  // Stand-ins for the ranks' pieces till the catalogue is designed: the steels in each rank's colours.
  knight_silver: { file: KNIGHT, swatches: { '3,0': 0xdfe3e8, '7,0': 0x9ea6b0 } },
  knight_gold: { file: KNIGHT, swatches: { '3,0': 0xe2b13c, '7,0': 0xa8781c } },
  knight_platinum: {
    file: KNIGHT,
    swatches: { '3,0': 0xeaf2f6, '7,0': 0xb4c8d6, '0,1': 0x5b6fc8 },
  },
  // Stand-ins for the trials' pieces: the weapons' steel, swatches 2,1 and 3,1, in bronze.
  knight_bronze: { file: KNIGHT, swatches: { '3,1': 0xb07a3c } },
  rogue_bronze: { file: ROGUE, swatches: { '2,1': 0xb07a3c, '3,1': 0xb07a3c } },
  mage_bronze: { file: MAGE, swatches: { '3,1': 0xb07a3c } },
  // Stand-ins for the feats' pieces: the Mage's hat in green, the Knight's plate in dark steel with a blue tabard.
  mage_green: { file: MAGE, swatches: { '1,1': 0x4f7f3a } },
  knight_veteran: { file: KNIGHT, swatches: { '3,0': 0x59626e, '7,0': 0x3a4049, '0,1': 0x2c5aa0 } },
  // The PvP win's stand-in: the Barbarian's hat in red fur with gold horns.
  barbarian_victor: { file: BARBARIAN, swatches: { '7,0': 0x8c2a24, '2,1': 0xd8a23e } },
  // The feathered wings in raven's colours: the feathers, their tips and the shade between.
  wings_raven: { file: WINGS, swatches: { '1,0': 0x2b2b33, '2,0': 0x5a4a7a, '3,0': 0x1c1c22 } },
  // The draft catalogue's colourways. Each file's roles: main is the swatch covering most of the piece, trim the detail beside it, accent the cloth or cape.
  // Knight: plate 3,0, the steel detail 7,0, cape and tabard 0,1; a helmet has the first two.
  knight_steel: { file: KNIGHT, swatches: { '3,0': 0x7d8fa6, '7,0': 0xd9dee6, '0,1': 0x3c4a5e } },
  knight_black: { file: KNIGHT, swatches: { '3,0': 0x2a2a30, '7,0': 0xb33a3a } },
  // Barbarian: fur 7,0, leather 6,0, the blue cloth and cape 0,1.
  barbarian_forest: {
    file: BARBARIAN,
    swatches: { '7,0': 0x4f6b33, '6,0': 0x8a6a3e, '0,1': 0x2e3b22 },
  },
  barbarian_frost: {
    file: BARBARIAN,
    swatches: { '7,0': 0xe8eef2, '6,0': 0x8fb8d8, '0,1': 0x5c7a94 },
  },
  // Rogue: leather 0,1, the strap leather 7,1, the cape and hood 1,1.
  rogue_shadow: { file: ROGUE, swatches: { '0,1': 0x2b2b33, '7,1': 0x6b6b7a, '1,1': 0xb33a3a } },
  rogue_dune: { file: ROGUE, swatches: { '0,1': 0xc9a46a, '7,1': 0x8a5a32, '1,1': 0xe8d6a8 } },
  rogue_ranger: {
    file: ROGUE_HOODED,
    swatches: { '0,1': 0x3f6e4a, '7,1': 0xa07c4a, '1,1': 0x22392a },
  },
  // Mage: robe 0,1, the belt and steel 4,0, the dark sleeves 7,1; its hat is 1,1 with a band 5,0 and a point 3,0.
  mage_night: { file: MAGE, swatches: { '0,1': 0x26315e, '4,0': 0xc9b458, '7,1': 0x141a36 } },
  mage_ember: { file: MAGE, swatches: { '0,1': 0xb8452a, '4,0': 0xf0a040, '7,1': 0x4a1a10 } },
  mage_tide: { file: MAGE, swatches: { '0,1': 0x2f8a9a, '4,0': 0xe0f0ec, '7,1': 0x1a4a55 } },
  mage_night_hat: { file: MAGE, swatches: { '1,1': 0x26315e, '5,0': 0xc9b458, '3,0': 0x141a36 } },
  // Weapons: a knife's blade 3,1 and 2,1 and its grip 6,1; the staff's wood 5,1 and gem 6,2.
  knife_shadow: { file: ROGUE, swatches: { '3,1': 0x3a3a44, '2,1': 0x3a3a44, '6,1': 0xb33a3a } },
  knife_gilded: { file: ROGUE, swatches: { '3,1': 0xe8c860, '2,1': 0xe8c860, '6,1': 0x5a2a6a } },
  staff_crystal: { file: MAGE, swatches: { '5,1': 0xcfd8e3, '6,2': 0x7fd0e0 } },
};
