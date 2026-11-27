import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NodeIO } from '@gltf-transform/core';
import type { Document } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { describe, expect, it } from 'vitest';

import { CELLS, PIECES, faceFile } from '../../../scripts/face.ts';

import { COLOURWAYS, PIECE_LOOKS } from './outfit-looks';

const SRC = path.resolve(process.cwd(), 'models-src/face/Face.glb');
const KAYKIT = path.resolve(process.cwd(), 'models-src/kaykit');
const io = new NodeIO().registerExtensions(ALL_EXTENSIONS);

const HEADS = {
  'kaykit/Barbarian.glb': 'Barbarian_Head',
  'kaykit/Knight.glb': 'Knight_Head',
  'kaykit/Mage.glb': 'Mage_Head',
  'kaykit/Rogue.glb': 'Rogue_Head',
  'kaykit/Rogue_Hooded.glb': 'Rogue_Head_Hooded',
} as const;

const looks = Object.entries(PIECE_LOOKS).filter(([, p]) => p.slot === 'face');

/** A mesh node's vertices, in the file's own axes. */
function vertices(doc: Document, name: string): number[][] {
  const node = doc
    .getRoot()
    .listNodes()
    .find((n) => n.getName() === name)!;
  return node
    .getMesh()!
    .listPrimitives()
    .flatMap((p) => {
      const a = p.getAttribute('POSITION')!;
      return Array.from({ length: a.getCount() }, (_, i) => a.getElement(i, [0, 0, 0]));
    });
}

/** A class's head, as the head joint's axes have it: its vertices through the joint's inverse bind. */
async function headOf(file: keyof typeof HEADS): Promise<number[][]> {
  const doc = await io.read(path.join(KAYKIT, file.slice('kaykit/'.length)));
  const node = doc
    .getRoot()
    .listNodes()
    .find((n) => n.getName() === HEADS[file])!;
  const skin = node.getSkin()!;
  const k = skin.listJoints().findIndex((j) => j.getName() === 'head');
  const inv = skin.getInverseBindMatrices()!.getElement(k, new Array<number>(16));
  return vertices(doc, HEADS[file]).map((v) =>
    [0, 1, 2].map(
      (r) => inv[r]! * v[0]! + inv[4 + r]! * v[1]! + inv[8 + r]! * v[2]! + inv[12 + r]!,
    ),
  );
}

describe('the face pieces made for the game', () => {
  it('are what `npm run face` makes: the committed file is its bytes', async () => {
    expect(Buffer.from(await faceFile()).equals(readFileSync(SRC))).toBe(true);
  });

  it('is four pieces, each a node of one mesh at the scene’s root, no skin, drawing only the atlas’s own swatches', async () => {
    const doc = await io.read(SRC);
    expect(doc.getRoot().listSkins()).toHaveLength(0);
    expect(
      doc
        .getRoot()
        .listNodes()
        .map((n) => n.getName())
        .sort(),
    ).toEqual([...PIECES].sort());
    const swatches = new Set<string>(Object.values(CELLS).map(([s]) => s));
    expect(swatches.has('0,0')).toBe(false); // the face's cell is the one a colourway never changes
    for (const n of doc.getRoot().listNodes()) {
      expect(n.getParentNode(), n.getName()).toBeNull();
      expect(n.getTranslation(), n.getName()).toEqual([0, 0, 0]);
      const uv = n.getMesh()!.listPrimitives()[0]!.getAttribute('TEXCOORD_0')!;
      for (let i = 0; i < uv.getCount(); i++) {
        const [u, v] = uv.getElement(i, [0, 0]);
        expect(swatches.has(`${Math.floor(u * 8)},${Math.floor(v * 4)}`), n.getName()).toBe(true);
      }
    }
  });

  it('has a look for every piece, each colourway recolouring only the swatches of the piece it is for', () => {
    expect(new Set(looks.flatMap(([, p]) => p.meshes))).toEqual(new Set(PIECES));
    for (const [key, p] of looks) {
      expect(p.file, key).toBe('face/Face.glb');
      if (p.colourway) {
        expect(COLOURWAYS[p.colourway]!.file, key).toBe(p.file);
      }
    }
  });

  it.each(Object.keys(HEADS) as (keyof typeof HEADS)[])(
    'sits on the %s’s face as worn: in front of it and never in it, no further off than a hand’s width, its sides on the head’s',
    async (file) => {
      const head = await headOf(file);
      const front = (x: number, y: number): number | null => {
        const near = head.filter((v) => Math.abs(v[0]! - x) < 0.08 && Math.abs(v[1]! - y) < 0.06);
        return near.length === 0 ? null : Math.max(...near.map((v) => v[2]!));
      };
      const doc = await io.read(SRC);
      for (const [key, p] of looks) {
        const fit = p.fit?.[file] ?? { offset: [0, 0, 0], scale: 1 };
        for (const v of vertices(doc, p.meshes[0]!)) {
          const x = v[0]! * fit.scale + fit.offset[0];
          const y = v[1]! * fit.scale + fit.offset[1];
          const z = v[2]! * fit.scale + fit.offset[2];
          const surface = Math.abs(x) < 0.28 && z > 0.25 ? front(x, y) : null;
          if (surface !== null) {
            expect(z, `${key} on ${file} at ${x.toFixed(2)}, ${y.toFixed(2)}`).toBeGreaterThan(
              surface - 0.03,
            );
            expect(z, `${key} on ${file} at ${x.toFixed(2)}, ${y.toFixed(2)}`).toBeLessThan(
              surface + 0.15,
            );
          }
          expect(Math.abs(x), `${key} on ${file}`).toBeLessThan(0.6); // never wider than the head and its ears
        }
      }
    },
  );
});
