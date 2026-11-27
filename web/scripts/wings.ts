// Makes the wings, our own work (outfits.md sections 6 and 8): `npm run wings` writes models-src/wings/Wings.glb, which `npm run models` turns into the parts file the client wears.
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { Document, NodeIO } from '@gltf-transform/core';
import sharp from 'sharp';

import {
  ATLAS_PX,
  atlasRaw,
  bar,
  faced,
  fan,
  flat,
  lerp,
  meshOf,
  mirrored,
  sub,
} from './lib/mesh.ts';
import type { P, Tri, V } from './lib/mesh.ts';

/** Where each half's root sits, in the chest's axes: behind the shoulder blade; the right half mirrors it. */
export const HINGE: V = [0.04, 0.15, -0.12];

/** How far across each pair spreads, tip to tip, in metres (outfits.md section 6). */
export const SPANS = {
  Feathered: 2.0,
  Bat: 2.2,
  Butterfly: 1.8,
  Dragon: 2.6,
  Crystal: 2.2,
} as const;
export type Kind = keyof typeof SPANS;
export const KINDS = Object.keys(SPANS) as Kind[];

/** The atlas's swatches, "column,row" from the top left of its 8 × 4 grid; 0,0 is the face's cell in KayKit's, which a colourway never changes, so none is drawn from it. */
export const CELLS = {
  feather: ['1,0', 0xf2f0ea],
  featherTip: ['2,0', 0xc9d6e0],
  featherShade: ['3,0', 0xdcd8cf],
  batMembrane: ['4,0', 0x5a2a3a],
  batBone: ['5,0', 0x2b2b33],
  batShade: ['6,0', 0x43202c],
  butterfly: ['7,0', 0x8fd3e8],
  butterflyEdge: ['0,1', 0x26315e],
  butterflySpot: ['1,1', 0xf0a040],
  butterflyShade: ['2,1', 0x6fb4cc],
  dragon: ['3,1', 0x9e2a2b],
  dragonBone: ['4,1', 0xd8b25a],
  dragonShade: ['5,1', 0x7a1f20],
  claw: ['6,1', 0x4a3a22],
  crystal: ['7,1', 0x8fd3e8],
  core: ['0,2', 0xe0f0ff],
  facet: ['1,2', 0x5fa6c0],
} as const satisfies Record<string, readonly [string, number]>;

const cell = (k: keyof typeof CELLS): string => CELLS[k][0];

const RAD = Math.PI / 180;

/** The point a fraction t along a polyline, by its vertices' spacing. */
function along(line: readonly P[], t: number): P {
  const k = Math.min(line.length - 2, Math.floor(t * (line.length - 1)));
  return lerp(line[k]!, line[k + 1]!, t * (line.length - 1) - k);
}

/** A feather from base, angle degrees below straight out, len long and w wide, its tip in the tip swatch. */
function feather(
  base: P,
  angle: number,
  len: number,
  w: number,
  z: number,
  main: string,
  tip: string,
): Tri[] {
  const d: P = [Math.cos(angle * RAD), -Math.sin(angle * RAD)];
  const n: P = [-d[1], d[0]];
  const mid: P = [base[0] + d[0] * len * 0.45, base[1] + d[1] * len * 0.45];
  const left: P = [mid[0] + (n[0] * w) / 2, mid[1] + (n[1] * w) / 2];
  const right: P = [mid[0] - (n[0] * w) / 2, mid[1] - (n[1] * w) / 2];
  const end: P = [base[0] + d[0] * len, base[1] + d[1] * len];
  return [flat(base, left, right, z, main), flat(left, end, right, z, tip)];
}

/** A membrane edge: the midpoint of a and b, drawn in toward w so the edge scallops between two fingers. */
const scallop = (a: P, b: P, w: P, pull: number): P => lerp(lerp(a, b, 0.5), w, pull);

/** Three rows of feathers fanned from a bent arm: the primaries behind, the secondaries over them, the coverts on top. */
function feathered(): Tri[] {
  const arm: P[] = [
    [0, 0],
    [0.32, 0.26],
    [0.66, 0.3],
    [0.98, 0.18],
  ];
  const tris: Tri[] = [];
  const row = (
    n: number,
    t0: number,
    t1: number,
    a0: number,
    a1: number,
    l0: number,
    l1: number,
    w: number,
    z: number,
    main: string,
    tip: string,
  ) => {
    for (let i = 0; i < n; i++) {
      const u = i / (n - 1);
      tris.push(
        ...feather(
          along(arm, t0 + (t1 - t0) * u),
          a0 + (a1 - a0) * u,
          l0 + (l1 - l0) * u,
          w,
          z,
          main,
          tip,
        ),
      );
    }
  };
  row(7, 1, 0.3, 22, 80, 0.66, 0.52, 0.17, 0, cell('featherShade'), cell('featherTip'));
  row(6, 0.95, 0.3, 30, 85, 0.44, 0.34, 0.15, -0.012, cell('feather'), cell('featherTip'));
  row(5, 0.9, 0.3, 35, 80, 0.24, 0.2, 0.13, -0.024, cell('feather'), cell('feather'));
  for (let i = 0; i < arm.length - 1; i++) {
    tris.push(...bar(arm[i]!, arm[i + 1]!, 0.045, -0.04, cell('featherShade')));
  }
  return tris;
}

/** Three fingers and an arm, the membrane stretched between them and down to the body, its edge scalloped. */
function bat(): Tri[] {
  const root: P = [0, 0];
  const wrist: P = [0.62, 0.42];
  const tips: P[] = [
    [1.0, 0.62],
    [1.06, 0.18],
    [0.88, -0.32],
  ];
  const body: P = [0.12, -0.5];
  const edge: P[] = [
    tips[0]!,
    scallop(tips[0]!, tips[1]!, wrist, 0.18),
    tips[1]!,
    scallop(tips[1]!, tips[2]!, wrist, 0.18),
    tips[2]!,
    scallop(tips[2]!, body, wrist, 0.15),
    body,
  ];
  const tris = fan([wrist, ...edge], 0, cell('batMembrane'));
  tris.push(flat(root, wrist, body, 0, cell('batShade')));
  tris.push(...bar(root, wrist, 0.05, -0.012, cell('batBone')));
  for (const tip of tips) {
    tris.push(...bar(wrist, tip, 0.032, -0.012, cell('batBone')));
  }
  tris.push(
    flat(
      [wrist[0] - 0.03, wrist[1]],
      [wrist[0] + 0.03, wrist[1]],
      [wrist[0] + 0.01, wrist[1] + 0.12],
      -0.02,
      cell('batBone'),
    ),
  );
  return tris;
}

/** An ellipse of n sides turned by rot degrees, scaled by k about its centre. */
function ring(c: P, rx: number, ry: number, rot: number, n: number, k: number): P[] {
  return Array.from({ length: n }, (_, i) => {
    const a = (2 * Math.PI * i) / n;
    const x = Math.cos(a) * rx * k;
    const y = Math.sin(a) * ry * k;
    return [
      c[0] + x * Math.cos(rot * RAD) - y * Math.sin(rot * RAD),
      c[1] + x * Math.sin(rot * RAD) + y * Math.cos(rot * RAD),
    ] as P;
  });
}

/** One lobe: an inner fan in its swatch and a rim of quads round it in the edge's. */
function lobe(c: P, rx: number, ry: number, rot: number, z: number, inner: string): Tri[] {
  const n = 10;
  const inside = ring(c, rx, ry, rot, n, 0.78);
  const outside = ring(c, rx, ry, rot, n, 1);
  const tris = fan(inside, z, inner);
  for (let i = 0; i < n; i++) {
    const j = (i + 1) % n;
    tris.push(
      flat(inside[i]!, outside[i]!, outside[j]!, z, cell('butterflyEdge')),
      flat(inside[i]!, outside[j]!, inside[j]!, z, cell('butterflyEdge')),
    );
  }
  return tris;
}

/** Two lobes a side, the upper the larger, with a few spots. */
function butterfly(): Tri[] {
  const tris = [
    ...lobe([0.52, 0.32], 0.48, 0.34, 18, 0, cell('butterfly')),
    ...lobe([0.36, -0.26], 0.32, 0.26, -20, -0.008, cell('butterflyShade')),
  ];
  for (const [c, r] of [
    [[0.6, 0.35], 0.06],
    [[0.42, 0.2], 0.045],
    [[0.38, -0.25], 0.05],
  ] as const) {
    tris.push(...fan(ring(c, r, r, 0, 6, 1), -0.02, cell('butterflySpot')));
  }
  return tris;
}

/** Bat-like but heavier: a thick leading bone with a claw at the wrist, four fingers each ending in a claw. */
function dragon(): Tri[] {
  const root: P = [0, 0];
  const elbow: P = [0.5, 0.42];
  const wrist: P = [0.95, 0.62];
  const tips: P[] = [
    [1.26, 0.8],
    [1.28, 0.3],
    [1.1, -0.18],
    [0.82, -0.55],
  ];
  const body: P = [0.14, -0.62];
  const edge: P[] = [tips[0]!];
  for (let i = 1; i < tips.length; i++) {
    edge.push(scallop(tips[i - 1]!, tips[i]!, wrist, 0.2), tips[i]!);
  }
  edge.push(scallop(tips[tips.length - 1]!, body, wrist, 0.18), body);
  const tris = fan([wrist, ...edge], 0, cell('dragon'));
  tris.push(
    flat(elbow, wrist, body, 0, cell('dragonShade')),
    flat(root, elbow, body, 0, cell('dragonShade')),
  );
  tris.push(
    ...bar(root, elbow, 0.09, -0.02, cell('dragonBone')),
    ...bar(elbow, wrist, 0.09, -0.02, cell('dragonBone')),
  );
  for (const tip of tips) {
    tris.push(...bar(wrist, tip, 0.045, -0.012, cell('dragonBone')));
    const len = Math.hypot(tip[0] - wrist[0], tip[1] - wrist[1]);
    const d: P = [(tip[0] - wrist[0]) / len, (tip[1] - wrist[1]) / len];
    tris.push(
      flat(
        [tip[0] - d[1] * 0.035, tip[1] + d[0] * 0.035],
        [tip[0] + d[1] * 0.035, tip[1] - d[0] * 0.035],
        [tip[0] + d[0] * 0.1, tip[1] + d[1] * 0.1],
        -0.02,
        cell('claw'),
      ),
    );
  }
  tris.push(
    flat(
      [wrist[0] - 0.04, wrist[1] + 0.02],
      [wrist[0] + 0.04, wrist[1] + 0.04],
      [wrist[0] + 0.02, wrist[1] + 0.3],
      -0.03,
      cell('claw'),
    ),
  );
  return tris;
}

/** A shard: a four-sided double point from near the root out to its tip, faceted in two swatches. */
function shard(angle: number, len: number, side: number): Tri[] {
  const d: V = [Math.cos(angle * RAD), Math.sin(angle * RAD), 0];
  const n: V = [-d[1], d[0], 0];
  const w = 0.1 * len + 0.04;
  const t = 0.035;
  const ringAt = 0.3 * len;
  const mid: V = [d[0] * ringAt, d[1] * ringAt, side];
  const ringPoints: V[] = [
    [mid[0] + n[0] * w, mid[1] + n[1] * w, mid[2]],
    [mid[0], mid[1], mid[2] + t],
    [mid[0] - n[0] * w, mid[1] - n[1] * w, mid[2]],
    [mid[0], mid[1], mid[2] - t],
  ];
  const tip: V = [d[0] * len, d[1] * len, side];
  const base: V = [d[0] * 0.08 * len, d[1] * 0.08 * len, side];
  const tris: Tri[] = [];
  for (let i = 0; i < 4; i++) {
    const a = ringPoints[i]!;
    const b = ringPoints[(i + 1) % 4]!;
    const out = (p: V): V => sub(p, mid);
    tris.push({
      v: [a, b, tip],
      cell: i % 2 ? cell('facet') : cell('crystal'),
      out: out([
        (a[0] + b[0] + tip[0]) / 3,
        (a[1] + b[1] + tip[1]) / 3,
        (a[2] + b[2] + tip[2]) / 3,
      ]),
    });
    tris.push({
      v: [base, b, a],
      cell: cell('core'),
      out: out([
        (a[0] + b[0] + base[0]) / 3,
        (a[1] + b[1] + base[1]) / 3,
        (a[2] + b[2] + base[2]) / 3,
      ]),
    });
  }
  return tris;
}

/** Shards fanned out of the root, the longest at the middle of the fan. */
function crystal(): Tri[] {
  const fanned: [number, number][] = [
    [70, 0.55],
    [48, 0.78],
    [28, 0.95],
    [12, 1.06],
    [-2, 1.05],
    [-16, 0.92],
    [-32, 0.76],
    [-50, 0.58],
    [-68, 0.42],
  ];
  return fanned.flatMap(([angle, len], i) => shard(angle, len, (i % 2 ? 1 : -1) * 0.012 * i));
}

const BUILD: Record<Kind, () => Tri[]> = {
  Feathered: feathered,
  Bat: bat,
  Butterfly: butterfly,
  Dragon: dragon,
  Crystal: crystal,
};

/** A pair's left half, its root at the origin and spreading toward +x, stretched across so its tips reach the pair's span. */
export function half(kind: Kind): Tri[] {
  const tris = BUILD[kind]();
  const farthest = Math.max(...tris.flatMap((t) => t.v.map((v) => v[0])));
  const k = (SPANS[kind] / 2 - HINGE[0]) / farthest;
  return tris.map((t) =>
    faced({ ...t, v: t.v.map((v): V => [v[0] * k, v[1], v[2]]) as unknown as Tri['v'] }),
  );
}

/** The pairs' file: one atlas, and each half a mesh on a node at its root. */
export async function wingsFile(): Promise<Uint8Array> {
  const png = await sharp(atlasRaw(CELLS), {
    raw: { width: ATLAS_PX, height: ATLAS_PX, channels: 3 },
  })
    .png({ compressionLevel: 9 })
    .toBuffer();
  const doc = new Document();
  const buffer = doc.createBuffer();
  const scene = doc.createScene();
  const texture = doc.createTexture('wings').setImage(png).setMimeType('image/png');
  const material = doc
    .createMaterial('wings')
    .setBaseColorTexture(texture)
    .setDoubleSided(true)
    .setRoughnessFactor(1)
    .setMetallicFactor(0);
  for (const kind of KINDS) {
    const left = half(kind);
    for (const [suffix, side, tris] of [
      ['L', 1, left],
      ['R', -1, mirrored(left)],
    ] as const) {
      const name = `${kind}_Wing_${suffix}`;
      const node = doc.createNode(name).setMesh(meshOf(doc, buffer, material, name, tris));
      scene.addChild(node.setTranslation([side * HINGE[0], HINGE[1], HINGE[2]]));
    }
  }
  return new NodeIO().writeBinary(doc);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const out = join(
    dirname(fileURLToPath(import.meta.url)),
    '..',
    'models-src',
    'wings',
    'Wings.glb',
  );
  mkdirSync(dirname(out), { recursive: true });
  const bytes = await wingsFile();
  writeFileSync(out, bytes);
  console.log(`models-src/wings/Wings.glb: ${(bytes.byteLength / 1024).toFixed(1)} KB`);
}
