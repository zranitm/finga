// A region's water (large-regions.md section 10): each river a ribbon along its smoothed path at each point's level, each lake its area at its level, both grown to meet their banks under the ground; flat, toon-drawn and see-through in the palette's water colour.
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Scene } from '@babylonjs/core/scene';
import earcut from 'earcut';

import type { Lake, Region, River } from './region-bake';
import { linearOf } from './toon';
import type { Palette } from './toon';

const BANK_M = 2; // past a river's channel or a lake's edge, so the surface runs under the banks' ground
const MITRE = 3; // a lake corner moves out at most this many times BANK_M, so a sharp one doesn't spike
const ALPHA = 0.85; // the ground under the shallows shows through

/** Arrays for a flat surface, its triangles wound as the ground's are, so it faces up. */
export interface WaterArrays {
  readonly positions: Float32Array;
  readonly indices: Uint32Array;
}

/** A river's surface: at each point of its path, a vertex either side across it, half its width and BANK_M out, at the point's level. */
export function riverArrays(r: River): WaterArrays {
  const n = r.path.length;
  const half = r.width / 2 + BANK_M;
  const positions: number[] = [];
  for (let i = 0; i < n; i++) {
    const [ax, az] = r.path[Math.max(i - 1, 0)]!;
    const [bx, bz] = r.path[Math.min(i + 1, n - 1)]!;
    const along = Math.hypot(bx - ax, bz - az) || 1;
    const [sx, sz] = [-(bz - az) / along, (bx - ax) / along]; // across it, to the left going downstream
    const [x, z] = r.path[i]!;
    const y = r.levels[i]!;
    positions.push(x + sx * half, y, z + sz * half, x - sx * half, y, z - sz * half);
  }
  const indices: number[] = [];
  for (let i = 0; i + 1 < n; i++) {
    const [l, rt, l2, r2] = [2 * i, 2 * i + 1, 2 * i + 2, 2 * i + 3];
    indices.push(l, rt, l2, rt, r2, l2); // the left bank first going downstream: wound as the ground is, whichever way it runs
  }
  return arrays(positions, indices);
}

/** A lake's surface: its area grown by BANK_M, each corner moved out along the line between its two sides', at its level, cut into triangles. */
export function lakeArrays(l: Lake): WaterArrays {
  const a = l.area;
  const n = a.length;
  let twice = 0; // twice the signed area, positive going counter-clockwise with x east and z north
  for (let i = 0; i < n; i++) {
    const [x0, z0] = a[i]!;
    const [x1, z1] = a[(i + 1) % n]!;
    twice += x0 * z1 - x1 * z0;
  }
  const out = Math.sign(twice) || 1;
  /** The outward unit normal of the side from p to q. */
  const normal = ([px, pz]: readonly [number, number], [qx, qz]: readonly [number, number]) => {
    const len = Math.hypot(qx - px, qz - pz) || 1;
    return [(out * (qz - pz)) / len, (out * -(qx - px)) / len] as const;
  };
  const flat: number[] = [];
  const positions: number[] = [];
  for (let i = 0; i < n; i++) {
    const prev = a[(i + n - 1) % n]!;
    const p = a[i]!;
    const next = a[(i + 1) % n]!;
    const [n1x, n1z] = normal(prev, p);
    const [n2x, n2z] = normal(p, next);
    const [bx, bz] = [n1x + n2x, n1z + n2z];
    const len = Math.hypot(bx, bz) || 1;
    const cos = (bx * n1x + bz * n1z) / len; // between the bisector and each side's normal
    const reach = Math.min(BANK_M / Math.max(cos, 1e-6), MITRE * BANK_M);
    const [x, z] = [p[0] + (bx / len) * reach, p[1] + (bz / len) * reach];
    flat.push(x, z);
    positions.push(x, l.level, z);
  }
  return arrays(positions, earcut(flat)); // earcut winds them as the ground is, whichever way the corners run
}

/** The arrays for positions and indices. */
function arrays(positions: readonly number[], indices: readonly number[]): WaterArrays {
  return { positions: Float32Array.from(positions), indices: Uint32Array.from(indices) };
}

/** Draws each of region's rivers and lakes under root, each its own mesh, toon-drawn in its water's colour, facing up, taking the sun's shadows. */
export function drawWater(
  scene: Scene,
  region: Pick<Region, 'water' | 'rivers' | 'lakes'>,
  palette: Palette,
  root: TransformNode,
): Mesh[] {
  const material = palette.toon({ texture: null, color: linearOf(region.water) }, ALPHA, true);
  const made = (name: string, a: WaterArrays) => {
    const data = new VertexData();
    data.positions = a.positions;
    data.indices = a.indices;
    data.normals = new Float32Array(a.positions.length).map((_, k) => (k % 3 === 1 ? 1 : 0));
    const m = new Mesh(name, scene);
    data.applyToMesh(m);
    m.parent = root;
    m.material = material;
    m.receiveShadows = true;
    m.isPickable = false;
    m.freezeWorldMatrix();
    return m;
  };
  return [
    ...region.rivers.map((r, i) => made(`river ${i}`, riverArrays(r))),
    ...region.lakes.map((l, i) => made(`lake ${i}`, lakeArrays(l))),
  ];
}
