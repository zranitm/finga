// Makes the face pieces, our own work (outfits.md sections 6 and 8): `npm run face` writes models-src/face/Face.glb, which `npm run models` turns into the parts file the client wears
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { Document, NodeIO } from '@gltf-transform/core';
import sharp from 'sharp';

import { ATLAS_PX, atlasRaw, beam, disc, meshOf, mirrored, path, ring, sheet } from './lib/mesh.ts';
import type { Tri, V } from './lib/mesh.ts';

/** The atlas's swatches, "column,row" from the top left of its 8 × 4 grid; each role has its own, so a colourway recolours a piece alone. */
export const CELLS = {
  rim: ['1,0', 0xc9a34a],
  patch: ['2,0', 0x1e1e22],
  patchStrap: ['3,0', 0x3a2a1e],
  cloth: ['4,0', 0x2b2b33],
  frame: ['5,0', 0xa8783a],
  lens: ['6,0', 0x7fd0e0],
  strap: ['7,0', 0x3a2a1e],
} as const satisfies Record<string, readonly [string, number]>;

const cell = (k: keyof typeof CELLS): string => CELLS[k][0];

/** Where the eyes are on KayKit's heads, in the head joint's axes (+x the wearer's left, +z forward): the middle of each, and the depth in front of the face the frames sit. */
export const EYE = { x: 0.18, y: 0.37 } as const;

/** The pieces, each a node of one mesh at the scene's root, its vertices in the head joint's axes. */
export const PIECES = ['Round_Glasses', 'Eye_Patch', 'Cloth_Mask', 'Goggles'] as const;
export type Piece = (typeof PIECES)[number];

/** Two 8-sided rims, a bridge between them and an arm from each to its ear. */
function roundGlasses(): Tri[] {
  const z0 = 0.52;
  const z1 = 0.55;
  const left = [
    ...ring([EYE.x, EYE.y], 0.14, 0.11, 8, z0, z1, cell('rim')),
    ...path(
      [
        [EYE.x + 0.14, EYE.y + 0.01, (z0 + z1) / 2],
        [0.46, EYE.y + 0.01, 0.3],
        [0.56, EYE.y + 0.01, 0],
      ],
      0.03,
      cell('rim'),
    ),
  ];
  return [
    ...left,
    ...mirrored(left),
    ...beam(
      [-0.05, EYE.y + 0.06, (z0 + z1) / 2],
      [0.05, EYE.y + 0.06, (z0 + z1) / 2],
      0.03,
      cell('rim'),
    ),
  ];
}

/** A disc over the left eye, a strap up across the forehead to the right and one down to the left ear. */
function eyePatch(): Tri[] {
  return [
    ...disc([EYE.x, EYE.y], 0.14, 8, 0.525, cell('patch')),
    ...path(
      [
        [EYE.x - 0.08, EYE.y + 0.12, 0.54],
        [-0.3, 0.6, 0.5],
        [-0.48, 0.6, 0.28],
        [-0.56, 0.58, 0],
      ],
      0.03,
      cell('patchStrap'),
    ),
    ...path(
      [
        [EYE.x + 0.12, EYE.y - 0.06, 0.5],
        [0.45, 0.3, 0.3],
        [0.56, 0.3, 0],
      ],
      0.03,
      cell('patchStrap'),
    ),
  ];
}

/** Where the mask's front lies, row by row down from the nose bridge to the chin, across x = ±0.46, ±0.34, ±0.18 and 0 (a little clear of the heads' faces). */
const MASK_ROWS = [
  [0.32, [0.27, 0.46, 0.53, 0.56]],
  [0.2, [0.27, 0.44, 0.54, 0.56]],
  [0.08, [0.19, 0.38, 0.5, 0.52]],
  [0, [0.2, 0.38, 0.47, 0.48]],
] as const;
const MASK_X = [0.46, 0.34, 0.18, 0] as const;

/** A sheet from the nose bridge to the chin, wrapped round to the cheeks, with a loop to each ear. */
function clothMask(): Tri[] {
  const rows = MASK_ROWS.map(([y, zs]): V[] => [
    ...MASK_X.map((x, i): V => [-x, y, zs[i]!]),
    ...MASK_X.slice(0, 3)
      .reverse()
      .map((x, i): V => [x === 0 ? 0 : x, y, zs[2 - i]!]),
  ]);
  const half = (side: 1 | -1): Tri[] =>
    path(
      [
        [side * 0.46, 0.22, 0.25],
        [side * 0.56, 0.22, 0],
      ],
      0.03,
      cell('cloth'),
    );
  return [...sheet(rows, [0, 0.15, -0.1], cell('cloth')), ...half(1), ...half(-1)];
}

/** Two thick round frames with a lens each, a bridge and a strap round the head. */
function goggles(): Tri[] {
  const z0 = 0.51;
  const z1 = 0.56;
  const left = [
    ...ring([EYE.x, EYE.y], 0.17, 0.12, 8, z0, z1, cell('frame')),
    ...disc([EYE.x, EYE.y], 0.12, 8, z0 + 0.005, cell('lens')),
    ...path(
      [
        [EYE.x + 0.17, EYE.y, (z0 + z1) / 2],
        [0.47, EYE.y, 0.3],
        [0.56, EYE.y, 0],
        [0.47, EYE.y, -0.35],
      ],
      0.035,
      cell('strap'),
    ),
  ];
  return [
    ...left,
    ...mirrored(left),
    ...path(
      [
        [-0.47, EYE.y, -0.35],
        [0, EYE.y, -0.55],
        [0.47, EYE.y, -0.35],
      ],
      0.035,
      cell('strap'),
    ),
    ...beam(
      [-0.05, EYE.y + 0.03, (z0 + z1) / 2],
      [0.05, EYE.y + 0.03, (z0 + z1) / 2],
      0.05,
      cell('frame'),
    ),
  ];
}

const BUILD: Record<Piece, () => Tri[]> = {
  Round_Glasses: roundGlasses,
  Eye_Patch: eyePatch,
  Cloth_Mask: clothMask,
  Goggles: goggles,
};

export const pieceTris = (piece: Piece): Tri[] => BUILD[piece]();

/** The pieces' file: one atlas, and each piece a mesh on a node at the scene's root. */
export async function faceFile(): Promise<Uint8Array> {
  const png = await sharp(atlasRaw(CELLS), {
    raw: { width: ATLAS_PX, height: ATLAS_PX, channels: 3 },
  })
    .png({ compressionLevel: 9 })
    .toBuffer();
  const doc = new Document();
  const buffer = doc.createBuffer();
  const scene = doc.createScene();
  const texture = doc.createTexture('face').setImage(png).setMimeType('image/png');
  const material = doc
    .createMaterial('face')
    .setBaseColorTexture(texture)
    .setDoubleSided(true)
    .setRoughnessFactor(1)
    .setMetallicFactor(0);
  for (const piece of PIECES) {
    scene.addChild(
      doc.createNode(piece).setMesh(meshOf(doc, buffer, material, piece, pieceTris(piece))),
    );
  }
  return new NodeIO().writeBinary(doc);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const out = join(dirname(fileURLToPath(import.meta.url)), '..', 'models-src', 'face', 'Face.glb');
  mkdirSync(dirname(out), { recursive: true });
  const bytes = await faceFile();
  writeFileSync(out, bytes);
  console.log(`models-src/face/Face.glb: ${(bytes.byteLength / 1024).toFixed(1)} KB`);
}
