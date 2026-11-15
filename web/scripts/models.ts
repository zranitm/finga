// Makes the models the client serves, public/models, from the packs' files in models-src: `npm run models`, never part of the build.
// Node runs this file directly (type stripping), so it may use only erasable TypeScript.
import { mkdirSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

import { Logger, NodeIO } from '@gltf-transform/core';
import type { Document } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { dedup, prune, quantize, resample } from '@gltf-transform/functions';

import { MODELS, clipsOf } from '../src/game/world3d/model-looks.ts';

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

const sources = glbs(srcDir);
const byFile = played();
const unknown = [...byFile.keys(), ...Object.keys(DROPPED)].filter((f) => !sources.includes(f));
if (unknown.length > 0) {
  throw new Error(`models: no ${unknown.join(', ')} in models-src`);
}
for (const stale of glbs(outDir).filter((f) => !sources.includes(f))) {
  rmSync(join(outDir, stale));
}

const io = new NodeIO().registerExtensions(ALL_EXTENSIONS);
const kb = (n: number) => `${(n / 1024).toFixed(1)} KB`;
for (const file of sources) {
  const doc = await io.read(join(srcDir, file));
  doc.setLogger(new Logger(Logger.Verbosity.WARN));
  const clips = byFile.get(file) ?? new Set<string>();
  strip(doc, file, clips);
  // Quaternius's rigs move by thousandths of a unit under a ×100 armature: resample's default 1e-4 flattened the slime's bob.
  // One quantization grid for the whole scene keeps a character's one skin whole; a grid per mesh splits it.
  await doc.transform(
    ...(clips.size > 0 ? [resample({ tolerance: 1e-7 })] : []),
    prune(),
    dedup(),
    quantize({ quantizationVolume: 'scene' }),
  );
  const bytes = await io.writeBinary(doc);
  const out = join(outDir, file);
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(out, bytes);
  console.log(
    `${relative(outDir, out)}: ${kb(statSync(join(srcDir, file)).size)} -> ${kb(bytes.byteLength)}, ${kb(gzipSync(bytes, { level: 9 }).byteLength)} gzipped`,
  );
}
