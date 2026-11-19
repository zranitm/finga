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

export const PIECE_LOOKS: Readonly<Record<string, PieceLook>> = {
  knight_body: { slot: 'outfit', file: KNIGHT, meshes: body('Knight') },
  knight_crimson: {
    slot: 'outfit',
    file: KNIGHT,
    meshes: body('Knight'),
    colourway: 'knight_crimson',
  },
  barbarian_body: { slot: 'outfit', file: BARBARIAN, meshes: body('Barbarian') },
  mage_body: { slot: 'outfit', file: MAGE, meshes: body('Mage') },
  rogue_body: { slot: 'outfit', file: ROGUE, meshes: body('Rogue') },
  knight_helmet: { slot: 'headwear', file: KNIGHT, meshes: ['Knight_Helmet'] },
  barbarian_hat: { slot: 'headwear', file: BARBARIAN, meshes: ['Barbarian_Hat'] },
  mage_hat: { slot: 'headwear', file: MAGE, meshes: ['Mage_Hat'] },
  two_handed_sword: { slot: 'weapon', file: KNIGHT, meshes: ['2H_Sword'] },
  two_handed_axe: { slot: 'weapon', file: BARBARIAN, meshes: ['2H_Axe'] },
  wand: { slot: 'weapon', file: MAGE, meshes: ['1H_Wand'] },
  crossbow_1h: { slot: 'weapon', file: ROGUE, meshes: ['1H_Crossbow'] },
};

export const COLOURWAYS: Readonly<Record<string, Colourway>> = {
  knight_crimson: { file: KNIGHT, swatches: { '3,0': 0x9b2335, '7,0': 0x5e1a24, '0,1': 0x2b2b33 } }, // the plate's two steels, the tabard dark
};
