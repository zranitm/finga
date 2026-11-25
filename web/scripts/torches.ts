// KayKit Dungeon Remastered 1.0's torches made into models-src's (underground-and-underwater.md section 11, step 3b): each texture shrunk 1024 -> 256 px as the chest's; torch_lit's flame triangles, told by their atlas colour, cut out into torch_flame; torch_lit's handle stretched into a stake, so at the characters' scale its cup stands 1.8 m up; every file's cup centre at its origin.
// Run: node scripts/torches.ts <dir> with the pack's torch_lit.gltf.glb and torch_mounted.gltf.glb, from addons/kaykit_dungeon_remastered/Assets/gltf/ at b0ca9bd9; it writes models-src/kaykit/torch_{lit,flame,mounted}.glb, then npm run models.
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { NodeIO } from '@gltf-transform/core';
import type { Document, Primitive } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { compactPrimitive, prune } from '@gltf-transform/functions';
import sharp from 'sharp';

import { TORCH_SCALE } from '../src/game/world3d/kit-models.ts';

import { shrink } from './atlas.ts';
import type { Raw } from './atlas.ts';

const from = process.argv[2];
if (!from) {
  throw new Error('torches: name the folder holding the pack’s torch files');
}
const out = fileURLToPath(new URL('../models-src/kaykit/', import.meta.url));
const io = new NodeIO().registerExtensions(ALL_EXTENSIONS);

const TEXTURE_PX = 256;
const CUP = 0.429; // torch_lit's cup centre over its origin, in its file's units
const MOUNTED_CUP: readonly [number, number, number] = [0, 0.43, 0.348]; // torch_mounted's, its bracket's plate at z 0
const FOOT = 0.395; // torch_lit's foot under its origin
const STAKE_FROM = -0.05; // the handle's shaft has no vertex between -0.26 and 0.159: everything below this moves down
const STAKE = 1.8 / TORCH_SCALE - (CUP + FOOT); // the stretch that puts the cup's centre 1.8 m over the foot

/** A texel of the flame: bright orange to yellow, little blue. */
const flameColour = (r: number, g: number, b: number) => r > 225 && g > 95 && b < 150;

/** Shrinks doc's one texture to TEXTURE_PX; returns it as read, for judging colours. */
async function shrunk(doc: Document): Promise<Raw> {
  const tex = doc.getRoot().listTextures()[0]!;
  const { data, info } = await sharp(tex.getImage()!).raw().toBuffer({ resolveWithObject: true });
  const raw: Raw = { data, width: info.width, height: info.height, channels: info.channels };
  const small = shrink(raw, TEXTURE_PX);
  const png = await sharp(small.data, {
    raw: { width: small.width, height: small.height, channels: small.channels as 3 | 4 },
  })
    .png({ compressionLevel: 9 })
    .toBuffer();
  tex.setImage(new Uint8Array(png));
  return raw;
}

/** Keeps prim's triangles whose colour, sampled at their uvs' middle, keep takes. */
function keepTriangles(prim: Primitive, raw: Raw, keep: (flame: boolean) => boolean): void {
  const uv = prim.getAttribute('TEXCOORD_0')!;
  const indices = prim.getIndices()!;
  const kept: number[] = [];
  for (let t = 0; t < indices.getCount(); t += 3) {
    const corners = [0, 1, 2].map((k) => indices.getScalar(t + k));
    const [u, v] = corners
      .map((i) => uv.getElement(i, [0, 0]))
      .reduce((s, a) => [s[0]! + a[0]! / 3, s[1]! + a[1]! / 3], [0, 0]);
    const x = Math.min(raw.width - 1, Math.floor(u! * raw.width));
    const y = Math.min(raw.height - 1, Math.floor(v! * raw.height));
    const o = (y * raw.width + x) * raw.channels;
    if (keep(flameColour(raw.data[o]!, raw.data[o + 1]!, raw.data[o + 2]!))) {
      kept.push(...corners);
    }
  }
  indices.setArray(new Uint32Array(kept));
  compactPrimitive(prim);
}

/** Moves each of prim's positions that below takes by d. */
function moved(prim: Primitive, d: readonly [number, number, number], below = Infinity): void {
  const pos = prim.getAttribute('POSITION')!;
  for (let i = 0; i < pos.getCount(); i++) {
    const [x, y, z] = pos.getElement(i, [0, 0, 0]);
    if (y < below) {
      pos.setElement(i, [x + d[0], y + d[1], z + d[2]]);
    }
  }
}

/** Reads file, shrinks its texture, shapes its one primitive and writes it as name. */
async function make(file: string, name: string, shape: (p: Primitive, raw: Raw) => void) {
  const doc = await io.read(join(from!, file));
  const raw = await shrunk(doc);
  const prim = doc.getRoot().listMeshes()[0]!.listPrimitives()[0]!;
  shape(prim, raw);
  await doc.transform(prune());
  await io.write(join(out, name), doc);
  console.log(`${name}: ${prim.getIndices()!.getCount() / 3} triangles`);
}

await make('torch_lit.gltf.glb', 'torch_lit.glb', (p, raw) => {
  keepTriangles(p, raw, (flame) => !flame);
  moved(p, [0, -STAKE, 0], STAKE_FROM);
  moved(p, [0, -CUP, 0]);
});
await make('torch_lit.gltf.glb', 'torch_flame.glb', (p, raw) => {
  keepTriangles(p, raw, (flame) => flame);
  moved(p, [0, -CUP, 0]);
});
await make('torch_mounted.gltf.glb', 'torch_mounted.glb', (p) => {
  moved(p, [-MOUNTED_CUP[0], -MOUNTED_CUP[1], -MOUNTED_CUP[2]]);
});
