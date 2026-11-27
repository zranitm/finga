import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NodeIO } from '@gltf-transform/core';
import type { Document } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import sharp from 'sharp';
import { describe, expect, it } from 'vitest';

import {
  changedSwatches,
  colourway,
  recolour,
  shrink,
  skinSwatches,
} from '../../../scripts/atlas.ts';
import type { Raw } from '../../../scripts/atlas.ts';

import { MODELS } from './model-looks';
import { COLOURWAYS, PIECE_LOOKS, colourwayFile, partsFile } from './outfit-looks';

const OUT = path.resolve(process.cwd(), 'public/models');
const SRC = path.resolve(process.cwd(), 'models-src');
const io = new NodeIO().registerExtensions(ALL_EXTENSIONS);
const read = (dir: string, file: string) => io.read(path.join(dir, file));

async function raw(image: Uint8Array): Promise<Raw> {
  const { data, info } = await sharp(image).raw().toBuffer({ resolveWithObject: true });
  return { data, width: info.width, height: info.height, channels: info.channels };
}

/** A file's rig: each joint's name, parent and inverse bind matrix. */
function rigOf(doc: Document): { name: string; parent: string | null; bind: number[] }[] {
  const skin = doc.getRoot().listSkins()[0]!;
  const binds = skin.getInverseBindMatrices()!;
  const joints = skin.listJoints();
  return joints.map((j, i) => ({
    name: j.getName(),
    parent: joints.find((p) => p.listChildren().includes(j))?.getName() ?? null,
    bind: binds.getElement(i, []),
  }));
}

describe('the outfit pieces as the client draws them', () => {
  it('finds every piece’s meshes in its parts file, which has no clips', async () => {
    for (const [key, piece] of Object.entries(PIECE_LOOKS)) {
      const doc = await read(OUT, partsFile(piece.file));
      const names = doc
        .getRoot()
        .listNodes()
        .filter((n) => n.getMesh())
        .map((n) => n.getName());
      expect(names, key).toEqual(expect.arrayContaining([...piece.meshes]));
      expect(doc.getRoot().listAnimations(), key).toEqual([]);
    }
  });

  it('gives every class file and parts file one rig, joint for joint, bind for bind, so a part swaps onto any of them', async () => {
    const files = [
      ...new Set(Object.values(MODELS).flatMap((m) => (m.own ? [m.file] : []))),
      ...new Set(Object.values(PIECE_LOOKS).map((p) => partsFile(p.file))),
    ];
    expect(files.length).toBeGreaterThan(5);
    const docs = await Promise.all(files.map(async (f) => ({ f, doc: await read(OUT, f) })));
    const skinned = docs.filter((d) => d.doc.getRoot().listSkins().length > 0);
    const [first, ...rest] = skinned.map((d) => rigOf(d.doc));
    expect(first).toHaveLength(41);
    for (const [i, rig] of rest.entries()) {
      expect(
        rig.map((j) => [j.name, j.parent]),
        skinned[i + 1]!.f,
      ).toEqual(first!.map((j) => [j.name, j.parent]));
      rig.forEach((j, k) =>
        j.bind.forEach((v, n) =>
          expect(v, `${skinned[i + 1]!.f} ${j.name}`).toBeCloseTo(first![k]!.bind[n]!, 6),
        ),
      );
    }
    // A file of rigid pieces alone, the Skeletons' weapons, has no skin: its joints are the rig's, each under the parent the rig gives it, and its meshes hang on them.
    const parentOf = new Map(first!.map((j) => [j.name, j.parent]));
    const rigid = docs.filter((d) => d.doc.getRoot().listSkins().length === 0);
    expect(rigid.length).toBeGreaterThan(0);
    for (const { f, doc } of rigid) {
      for (const n of doc.getRoot().listNodes()) {
        const parent = n.getParentNode()?.getName();
        if (n.getMesh()) {
          expect(parentOf.has(parent ?? ''), `${f} ${n.getName()} hangs on ${parent}`).toBe(true);
        } else if (parentOf.get(n.getName())) {
          expect(parent, `${f} ${n.getName()}`).toBe(parentOf.get(n.getName())); // the rig's root hangs on the scene's Rig node
        }
      }
    }
  });

  /** Each source file's atlas, decoded once for all its colourways. */
  const sources = new Map<string, Promise<Raw>>();
  const sourceAtlas = (file: string): Promise<Raw> => {
    let atlas = sources.get(file);
    if (!atlas) {
      atlas = read(SRC, file).then((doc) => raw(doc.getRoot().listTextures()[0]!.getImage()!));
      sources.set(file, atlas);
    }
    return atlas;
  };

  it.each(Object.entries(COLOURWAYS))(
    'writes colourway %s as its file’s atlas at 128 px with its own swatches recoloured and nothing else, skin least of all',
    async (key, c) => {
      const atlas = await sourceAtlas(c.file);
      const shipped = await raw(readFileSync(path.join(OUT, colourwayFile(key))));
      expect([shipped.width, shipped.height]).toEqual([128, 128]);
      const plain = shrink(atlas, 128);
      const changed = changedSwatches(plain, shipped);
      expect([...changed].sort()).toEqual(Object.keys(c.swatches).sort());
      expect([...skinSwatches(atlas)].filter((s) => changed.has(s))).toEqual([]);
      expect(changedSwatches(shrink(recolour(atlas, c.swatches), 128), shipped)).toEqual(new Set()); // as the script writes it today
    },
  );
});

describe('a KayKit atlas', () => {
  const cell = (r: number, g: number, b: number) => [r, g, b, 255];
  /** An 8 × 4 atlas of one pixel a swatch, from rows of colours. */
  const atlasOf = (rows: number[][][]): Raw => ({
    data: Uint8Array.from(rows.flat(2)),
    width: 8,
    height: 4,
    channels: 4,
  });
  const plain = atlasOf(
    Array.from({ length: 4 }, (_, y) =>
      Array.from({ length: 8 }, (_, x) => cell(10 * x + 40, 10 * y + 40, 60)),
    ),
  );

  it('counts as skin the face swatch and any swatch sharing a colour with it', () => {
    const rows = Array.from({ length: 4 }, (_, y) =>
      Array.from({ length: 8 }, (_, x) => cell(10 * x + 40, 10 * y + 40, 60)),
    );
    rows[2]![7] = cell(40, 40, 60); // a mage's hands
    expect([...skinSwatches(atlasOf(rows))].sort()).toEqual(['0,0', '7,2']);
  });

  it('makes a colourway of any swatch but skin, refusing the face’s and one sharing its colours', () => {
    const rows = Array.from({ length: 4 }, (_, y) =>
      Array.from({ length: 8 }, (_, x) => cell(10 * x + 40, 10 * y + 40, 60)),
    );
    rows[2]![7] = cell(40, 40, 60); // a mage's hands
    const atlas = atlasOf(rows);
    expect(colourway(atlas, 'red', { '3,1': 0xff0000 })).toEqual(
      recolour(atlas, { '3,1': 0xff0000 }),
    );
    expect(() => colourway(atlas, 'pale', { '0,0': 0xffffff })).toThrow(
      'colourway pale changes skin, 0,0',
    );
    expect(() => colourway(atlas, 'gloves', { '3,1': 0xff0000, '7,2': 0 })).toThrow(
      'colourway gloves changes skin, 7,2',
    );
  });

  it('recolours a swatch to its new colour, scaled by each pixel’s lightness against the swatch’s mean', () => {
    const tall: Raw = {
      data: Uint8Array.from(
        Array.from({ length: 8 }, (_, y) =>
          Array.from({ length: 8 }, (_, x) =>
            x === 0 && y < 2 ? cell(200 - 100 * y, 0, 0) : cell(1, 2, 3),
          ),
        ).flat(2),
      ),
      width: 8,
      height: 8,
      channels: 4,
    }; // swatch 0,0 a gradient of two pixels
    const out = recolour(tall, { '0,0': 0x204060 });
    const at = (x: number, y: number) => [...out.data.slice((y * 8 + x) * 4, (y * 8 + x) * 4 + 4)];
    expect(at(0, 0)).toEqual([43, 85, 128, 255]); // 4/3 of the colour
    expect(at(0, 1)).toEqual([21, 43, 64, 255]); // 2/3
    expect(at(1, 0)).toEqual([1, 2, 3, 255]);
  });

  it('finds the swatches whose pixels differ in any channel', () => {
    const bluer = Uint8Array.from(plain.data);
    bluer[(2 * 8 + 5) * 4 + 2] = 99; // swatch 5,2's blue
    bluer[(0 * 8 + 1) * 4 + 1] = 0; // swatch 1,0's green
    expect(changedSwatches(plain, { ...plain, data: bluer })).toEqual(new Set(['5,2', '1,0']));
    expect(changedSwatches(plain, plain)).toEqual(new Set());
  });

  it('shrinks by each block’s mean, refusing a size that splits a swatch', () => {
    const striped: Raw = {
      data: Uint8Array.from(
        Array.from({ length: 16 * 16 }, (_, i) => [i % 2 ? 10 : 30, 0, 0, 255]).flat(),
      ),
      width: 16,
      height: 16,
      channels: 4,
    }; // columns of 30 and 10
    const small = shrink(striped, 8);
    expect([small.width, small.height]).toEqual([8, 8]);
    expect(new Set(Array.from({ length: 64 }, (_, i) => small.data[i * 4]))).toEqual(new Set([20]));
    expect(() => shrink(plain, 6)).toThrow('whole swatches');
  });
});
