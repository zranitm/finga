// Makes the models the client serves, public/models, from the packs' files in models-src: `npm run models`, never part of the build.
import { existsSync, mkdirSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

import { Logger, NodeIO } from '@gltf-transform/core';
import type { Document } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { dedup, prune, quantize, resample } from '@gltf-transform/functions';
import sharp from 'sharp';

import { MODELS, clipsOf } from '../src/game/world3d/model-looks.ts';
import {
  COLOURWAYS,
  PIECE_LOOKS,
  colourwayFile,
  partsFile,
} from '../src/game/world3d/outfit-looks.ts';

import { colourway, shrink } from './atlas.ts';
import type { Raw } from './atlas.ts';

const here = dirname(fileURLToPath(import.meta.url));
const srcDir = join(here, '..', 'models-src');
const outDir = join(here, '..', 'public', 'models');

/** Each character's gear that no look shows, left out with its meshes; gear one look hides and another shows stays, in the first's hide list. */
const DROPPED: Readonly<Record<string, readonly string[]>> = {
  'kaykit/Barbarian.glb': ['Barbarian_Round_Shield', '1H_Axe_Offhand', '2H_Axe', 'Mug'],
  'kaykit/Knight.glb': [
    '1H_Sword_Offhand',
    'Badge_Shield',
    'Rectangle_Shield',
    'Round_Shield',
    'Spike_Shield',
    '2H_Sword',
  ],
  'kaykit/Mage.glb': ['Spellbook', 'Spellbook_open', '1H_Wand'],
  'kaykit/Rogue.glb': ['Knife_Offhand', '1H_Crossbow', '2H_Crossbow', 'Throwable'],
  'kaykit/Rogue_Hooded.glb': ['Knife', 'Knife_Offhand', '1H_Crossbow', 'Throwable'],
};

const COLOURWAY_PX = 128;

/** The .glb files under dir, as paths relative to it. */
function glbs(dir: string): string[] {
  return readdirSync(dir, { recursive: true, encoding: 'utf8' })
    .filter((f) => f.endsWith('.glb'))
    .map((f) => f.split('\\').join('/'))
    .sort();
}

/** The clips each file's looks play; a file no look names, a kit model, plays none. */
function played(): Map<string, Set<string>> {
  const byFile = new Map<string, Set<string>>();
  for (const look of Object.values(MODELS)) {
    const clips = byFile.get(look.file) ?? new Set<string>();
    for (const c of clipsOf(look)) {
      clips.add(c);
    }
    byFile.set(look.file, clips);
  }
  return byFile;
}

/** Leaves out the clips no look plays and the gear no look shows; a name the file lacks is an error, as a look out of step with its file. */
function strip(doc: Document, file: string, clips: ReadonlySet<string>): void {
  const root = doc.getRoot();
  const names = new Set(root.listAnimations().map((a) => a.getName()));
  const noClip = [...clips].filter((c) => !names.has(c));
  if (noClip.length > 0) {
    throw new Error(`models: ${file} has no clip ${noClip.join(', ')}`);
  }
  for (const a of root.listAnimations()) {
    if (!clips.has(a.getName())) {
      a.listChannels().forEach((c) => c.dispose());
      a.listSamplers().forEach((s) => s.dispose()); // else prune keeps their keyframes
      a.dispose();
    }
  }
  const drop = DROPPED[file] ?? [];
  const gear = root.listNodes().filter((n) => drop.includes(n.getName()));
  const noGear = drop.filter((d) => !gear.some((n) => n.getName() === d));
  if (noGear.length > 0) {
    throw new Error(`models: ${file} has no ${noGear.join(', ')}`);
  }
  for (const g of gear) {
    g.traverse((n) => n.getMesh()?.dispose());
    g.dispose();
  }
}

/** The meshes each source file's parts file keeps: every one an outfit piece wears. */
function worn(): Map<string, Set<string>> {
  const byFile = new Map<string, Set<string>>();
  for (const piece of Object.values(PIECE_LOOKS)) {
    const meshes = byFile.get(piece.file) ?? new Set<string>();
    piece.meshes.forEach((m) => meshes.add(m));
    byFile.set(piece.file, meshes);
  }
  return byFile;
}

/** Leaves the meshes pieces wear and the rig they're bound to; a mesh the file lacks is an error, as a look out of step with its file. */
function stripToParts(doc: Document, file: string, meshes: ReadonlySet<string>): void {
  const root = doc.getRoot();
  for (const a of root.listAnimations()) {
    a.listChannels().forEach((c) => c.dispose());
    a.listSamplers().forEach((s) => s.dispose());
    a.dispose();
  }
  const drawn = root.listNodes().filter((n) => n.getMesh());
  const missing = [...meshes].filter((m) => !drawn.some((n) => n.getName() === m));
  if (missing.length > 0) {
    throw new Error(`models: ${file} has no ${missing.join(', ')}`);
  }
  for (const n of drawn.filter((d) => !meshes.has(d.getName()))) {
    n.getMesh()?.dispose();
    n.dispose();
  }
}

/** Writes doc to out under the models, saying how much it shrank from source bytes. */
async function write(doc: Document, out: string, source: number): Promise<void> {
  const bytes = await io.writeBinary(doc);
  const path = join(outDir, out);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, bytes);
  console.log(
    `${out}: ${kb(source)} -> ${kb(bytes.byteLength)}, ${kb(gzipSync(bytes, { level: 9 }).byteLength)} gzipped`,
  );
}

/** A file's one atlas as raw pixels. */
async function atlasOf(doc: Document, file: string): Promise<Raw> {
  const textures = doc.getRoot().listTextures();
  const image = textures[0]?.getImage();
  if (textures.length !== 1 || !image) {
    throw new Error(`models: ${file} has ${textures.length} atlases, not one`);
  }
  const { data, info } = await sharp(image).raw().toBuffer({ resolveWithObject: true });
  return { data, width: info.width, height: info.height, channels: info.channels };
}

const sources = glbs(srcDir);
const byFile = played();
const byParts = worn();
const unknown = [
  ...byFile.keys(),
  ...Object.keys(DROPPED),
  ...byParts.keys(),
  ...Object.values(COLOURWAYS).map((c) => c.file),
].filter((f) => !sources.includes(f));
if (unknown.length > 0) {
  throw new Error(`models: no ${[...new Set(unknown)].join(', ')} in models-src`);
}
const parts = [...byParts.keys()].map(partsFile);
for (const stale of glbs(outDir).filter((f) => !sources.includes(f) && !parts.includes(f))) {
  rmSync(join(outDir, stale));
}
const colourways = Object.keys(COLOURWAYS).map(colourwayFile);
const colourwayDir = dirname(join(outDir, colourwayFile('_')));
if (existsSync(colourwayDir)) {
  for (const stale of readdirSync(colourwayDir).filter(
    (f) => !colourways.includes(relative(outDir, join(colourwayDir, f))),
  )) {
    rmSync(join(colourwayDir, stale));
  }
}

// A worn piece is bound by swapping its rig for the wearer's, so the files it moves between keep the packs' own vertex positions: quantizing them would fold each file's own grid into its rig.
const rigged = new Set(Object.values(MODELS).flatMap((m) => (m.own ? [m.file] : [])));
const quantizing = (file: string) =>
  quantize({
    quantizationVolume: 'scene', // one grid for the scene keeps each skin whole
    ...(rigged.has(file) && { pattern: /^(?!POSITION$)/ }),
  });

const io = new NodeIO().registerExtensions(ALL_EXTENSIONS);
const kb = (n: number) => `${(n / 1024).toFixed(1)} KB`;
for (const file of sources) {
  const doc = await io.read(join(srcDir, file));
  doc.setLogger(new Logger(Logger.Verbosity.WARN));
  const clips = byFile.get(file) ?? new Set<string>();
  strip(doc, file, clips);
  // A 1e-7 resample keeps Quaternius's tiny bone moves.
  await doc.transform(
    ...(clips.size > 0 ? [resample({ tolerance: 1e-7 })] : []),
    prune(),
    dedup(),
    quantizing(file),
  );
  await write(doc, file, statSync(join(srcDir, file)).size);
}
for (const [file, meshes] of byParts) {
  if (!rigged.has(file)) {
    throw new Error(`models: ${file} has pieces but no class wears its rig`);
  }
  const doc = await io.read(join(srcDir, file));
  doc.setLogger(new Logger(Logger.Verbosity.WARN));
  stripToParts(doc, file, meshes);
  await doc.transform(prune(), dedup(), quantizing(file));
  await write(doc, partsFile(file), statSync(join(srcDir, file)).size);
}
for (const [key, c] of Object.entries(COLOURWAYS)) {
  const atlas = await atlasOf(await io.read(join(srcDir, c.file)), c.file);
  const out = shrink(colourway(atlas, key, c.swatches), COLOURWAY_PX);
  const png = await sharp(out.data, {
    raw: { width: out.width, height: out.height, channels: out.channels as 3 | 4 },
  })
    .png({ compressionLevel: 9 })
    .toBuffer();
  const path = join(outDir, colourwayFile(key));
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, png);
  console.log(`${colourwayFile(key)}: ${kb(png.byteLength)}`);
}
