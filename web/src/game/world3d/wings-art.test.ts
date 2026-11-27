import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NodeIO } from '@gltf-transform/core';
import type { Document, Node } from '@gltf-transform/core';
import { describe, expect, it } from 'vitest';

import { CELLS, HINGE, KINDS, SPANS, half, wingsFile } from '../../../scripts/wings.ts';

import { COLOURWAYS, PIECE_LOOKS } from './outfit-looks';

const SRC = path.resolve(process.cwd(), 'models-src/wings/Wings.glb');
const io = new NodeIO();
const swatches = Object.values(CELLS).map(([s]) => s);

/** The committed file, read once. */
const committed: Promise<Document> = io.read(SRC);

/** A half's vertices, as it lies from its root. */
function vertices(node: Node): number[][] {
  const position = node.getMesh()!.listPrimitives()[0]!.getAttribute('POSITION')!;
  return Array.from({ length: position.getCount() }, (_, i) => position.getElement(i, []));
}

function nodeOf(doc: Document, name: string): Node {
  const node = doc
    .getRoot()
    .listNodes()
    .find((n) => n.getName() === name);
  if (!node) {
    throw new Error(`no node ${name}`);
  }
  return node;
}

describe('the wings made for the game', () => {
  it('are what `npm run wings` makes: the committed file is its bytes', async () => {
    expect(Buffer.from(await wingsFile()).equals(readFileSync(SRC))).toBe(true);
  });

  it('is five pairs, each half a node named for its side, its root at the hinge behind the chest on its side', async () => {
    const doc = await committed;
    expect(
      doc
        .getRoot()
        .listNodes()
        .map((n) => n.getName())
        .sort(),
    ).toEqual(KINDS.flatMap((k) => [`${k}_Wing_L`, `${k}_Wing_R`]).sort());
    for (const kind of KINDS) {
      expect(nodeOf(doc, `${kind}_Wing_L`).getTranslation()).toEqual([
        HINGE[0],
        HINGE[1],
        HINGE[2],
      ]);
      expect(nodeOf(doc, `${kind}_Wing_R`).getTranslation()).toEqual([
        -HINGE[0],
        HINGE[1],
        HINGE[2],
      ]);
    }
  });

  it.each(KINDS)(
    'spread the %s pair across its span, each half out to its side from its root',
    async (kind) => {
      const doc = await committed;
      const left = vertices(nodeOf(doc, `${kind}_Wing_L`));
      const right = vertices(nodeOf(doc, `${kind}_Wing_R`));
      expect(Math.min(...left.map((v) => v[0]!))).toBeGreaterThanOrEqual(-HINGE[0]); // never across the chest's middle
      expect(Math.max(...right.map((v) => v[0]!))).toBeLessThanOrEqual(HINGE[0]);
      const reach = Math.max(...left.map((v) => v[0]!));
      expect(2 * (reach + HINGE[0]), kind).toBeCloseTo(SPANS[kind], 3);
      expect(Math.min(...right.map((v) => v[0]!))).toBeCloseTo(-reach, 5);
    },
  );

  it.each(KINDS)(
    'keep the %s pair low-poly, and flat across the back but for its thickness',
    (kind) => {
      const tris = half(kind);
      expect(tris.length).toBeGreaterThan(10);
      expect(tris.length).toBeLessThan(300);
      const depth = Math.max(...tris.flatMap((t) => t.v.map((v) => Math.abs(v[2]))));
      expect(depth).toBeLessThan(kind === 'Crystal' ? 0.15 : 0.05);
    },
  );

  it('draw every triangle in a swatch of the atlas, none in the face’s, and use each swatch', async () => {
    const doc = await committed;
    const used = new Set<string>();
    for (const node of doc.getRoot().listNodes()) {
      const uv = node.getMesh()!.listPrimitives()[0]!.getAttribute('TEXCOORD_0')!;
      for (let i = 0; i < uv.getCount(); i++) {
        const [u, v] = uv.getElement(i, [0, 0]);
        used.add(`${Math.floor(u * 8)},${Math.floor(v * 4)}`);
        expect((u * 8) % 1, 'at a swatch’s middle').toBeCloseTo(0.5, 5);
        expect((v * 4) % 1, 'at a swatch’s middle').toBeCloseTo(0.5, 5);
      }
    }
    expect(used.has('0,0')).toBe(false);
    expect([...used].sort()).toEqual([...swatches].sort());
  });

  it('face a flat pair’s every triangle to the back, where the camera is', () => {
    for (const kind of KINDS.filter((k) => k !== 'Crystal')) {
      for (const t of half(kind)) {
        const [a, b, c] = t.v;
        const nz = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
        expect(nz, kind).toBeLessThan(0);
      }
    }
  });
});

describe('the wings as served', () => {
  it('keep each half’s node and hinge as made: the parts file `npm run models` writes', async () => {
    const made = await committed;
    const served = await io.read(
      path.resolve(process.cwd(), 'public/models/wings/parts/Wings.glb'),
    );
    for (const node of made.getRoot().listNodes()) {
      const same = nodeOf(served, node.getName());
      expect(same.getTranslation(), node.getName()).toEqual(node.getTranslation());
      expect(vertices(same).length, node.getName()).toBe(vertices(node).length);
    }
  });
});

describe('the wings’ looks', () => {
  const pairs = {
    wings_feather: 'Feathered',
    wings_raven: 'Feathered',
    wings_bat: 'Bat',
    wings_butterfly: 'Butterfly',
    wings_dragon: 'Dragon',
    wings_crystal: 'Crystal',
  };

  it('name each pair’s two halves, and are wings', () => {
    for (const [key, kind] of Object.entries(pairs)) {
      expect(PIECE_LOOKS[key], key).toMatchObject({
        slot: 'wings',
        file: 'wings/Wings.glb',
        meshes: [`${kind}_Wing_L`, `${kind}_Wing_R`],
      });
    }
    expect(Object.entries(PIECE_LOOKS).filter(([, p]) => p.slot === 'wings').length).toBe(6);
  });

  it('glow on the dragon’s and the crystal’s only, and Raven recolours the feathered pair’s own swatches', () => {
    expect(
      Object.keys(pairs)
        .filter((k) => PIECE_LOOKS[k]!.glow)
        .sort(),
    ).toEqual(['wings_crystal', 'wings_dragon']);
    expect(PIECE_LOOKS.wings_raven!.colourway).toBe('wings_raven');
    expect(Object.keys(COLOURWAYS.wings_raven!.swatches).sort()).toEqual(
      [CELLS.feather[0], CELLS.featherTip[0], CELLS.featherShade[0]].sort(),
    );
  });
});
