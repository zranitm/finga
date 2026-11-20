// A map's solids bucketed by 8 m squares, as the server's sim.Indexed buckets them (large-regions.md section 5): a move or a point reads only the buckets it touches.
import type { Box, Cylinder, Geometry } from './step';

/** The side of a solids bucket in metres. */
const BUCKET_M = 8;

/** How far a solid's bounds reach past its footprint, past any rounding in its footprint's tests. */
const BOUNDS_PAD = 1e-6;

/** A geometry's boxes and cylinders by the buckets their bounds overlap, each bucket's indices ascending. */
export interface Solids {
  readonly nx: number;
  readonly nz: number;
  readonly boxes: readonly (readonly number[])[];
  readonly cylinders: readonly (readonly number[])[];
}

const buckets = (size: number) => Math.max(1, Math.ceil(size / BUCKET_M));

/** The bucket along an axis of n holding v, held to the map: past an edge is the edge's bucket. */
function cell(v: number, n: number): number {
  const c = Math.floor(v / BUCKET_M);
  return c < 0 ? 0 : c >= n ? n - 1 : c;
}

/** Solids buckets being filled. */
export interface FilledSolids extends Solids {
  readonly boxes: number[][];
  readonly cylinders: number[][];
}

/** Empty buckets over a sizeX by sizeZ map. */
export function solidBuckets(sizeX: number, sizeZ: number): FilledSolids {
  const nx = buckets(sizeX);
  const nz = buckets(sizeZ);
  return {
    nx,
    nz,
    boxes: Array.from({ length: nx * nz }, () => []),
    cylinders: Array.from({ length: nx * nz }, () => []),
  };
}

/** Files solid i in each bucket of list its bounds overlap, each bucket's indices kept ascending. */
function file(
  s: FilledSolids,
  list: number[][],
  i: number,
  x0: number,
  z0: number,
  x1: number,
  z1: number,
) {
  for (let iz = cell(z0, s.nz); iz <= cell(z1, s.nz); iz++) {
    for (let ix = cell(x0, s.nx); ix <= cell(x1, s.nx); ix++) {
      const bucket = list[iz * s.nx + ix]!;
      let k = bucket.length;
      while (k > 0 && bucket[k - 1]! > i) {
        k--;
      }
      bucket.splice(k, 0, i);
    }
  }
}

/** Files box i, b, in s by its bounds. */
export function fileBox(s: FilledSolids, i: number, b: Box): void {
  const ex = Math.abs(b.ux) * b.half_w + Math.abs(b.uz) * b.half_d + BOUNDS_PAD;
  const ez = Math.abs(b.uz) * b.half_w + Math.abs(b.ux) * b.half_d + BOUNDS_PAD;
  file(s, s.boxes, i, b.x - ex, b.z - ez, b.x + ex, b.z + ez);
}

/** Files cylinder i, c, in s by its bounds. */
export function fileCylinder(s: FilledSolids, i: number, c: Cylinder): void {
  const r = c.radius + BOUNDS_PAD;
  file(s, s.cylinders, i, c.x - r, c.z - r, c.x + r, c.z + r);
}

/** g with its boxes and cylinders bucketed, which the sim's queries read: they come in their own order, each once, so a result is a full scan's to the bit. */
export function indexed(g: Geometry): Geometry {
  const s = solidBuckets(g.size_x, g.size_z);
  g.boxes.forEach((b, i) => fileBox(s, i, b));
  g.cylinders.forEach((c, i) => fileCylinder(s, i, c));
  return { ...g, buckets: s };
}

/** What a query over several buckets gathers, reused: a caller reads it before its next query. */
const gathered: number[] = [];

/** The boxes', or the cylinders', indices whose bounds overlap [x0, x1] by [z0, z1], ascending and each once; every one for g not indexed. */
export function near(
  g: Geometry,
  cylinders: boolean,
  x0: number,
  z0: number,
  x1: number,
  z1: number,
): readonly number[] {
  const s = g.buckets;
  if (!s) {
    return (cylinders ? g.cylinders : g.boxes).map((_, i) => i);
  }
  const list = cylinders ? s.cylinders : s.boxes;
  const ix0 = cell(x0, s.nx);
  const ix1 = cell(x1, s.nx);
  const iz0 = cell(z0, s.nz);
  const iz1 = cell(z1, s.nz);
  if (ix0 === ix1 && iz0 === iz1) {
    return list[iz0 * s.nx + ix0]!;
  }
  gathered.length = 0;
  for (let iz = iz0; iz <= iz1; iz++) {
    for (let ix = ix0; ix <= ix1; ix++) {
      for (const i of list[iz * s.nx + ix]!) {
        let k = gathered.length;
        while (k > 0 && gathered[k - 1]! > i) {
          k--;
        }
        if (k > 0 && gathered[k - 1] === i) {
          continue; // in a bucket already read
        }
        gathered.push(i);
        gathered.copyWithin(k + 1, k, gathered.length - 1);
        gathered[k] = i;
      }
    }
  }
  return gathered;
}
