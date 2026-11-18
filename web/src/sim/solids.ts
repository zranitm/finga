// A map's solids bucketed by 8 m squares, as the server's sim.Indexed buckets them (large-regions.md section 5): a move or a point reads only the buckets it touches.
import type { Geometry } from './step';

/** The side of a solids bucket in metres. */
export const BUCKET_M = 8;

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

/** g with its boxes and cylinders bucketed, which the sim's queries read: they come in their own order, each once, so a result is a full scan's to the bit. */
export function indexed(g: Geometry): Geometry {
  const nx = buckets(g.size_x);
  const nz = buckets(g.size_z);
  const boxes: number[][] = Array.from({ length: nx * nz }, () => []);
  const cylinders: number[][] = Array.from({ length: nx * nz }, () => []);
  const add = (list: number[][], i: number, x0: number, z0: number, x1: number, z1: number) => {
    for (let iz = cell(z0, nz); iz <= cell(z1, nz); iz++) {
      for (let ix = cell(x0, nx); ix <= cell(x1, nx); ix++) {
        list[iz * nx + ix]!.push(i);
      }
    }
  };
  g.boxes.forEach((b, i) => {
    const ex = Math.abs(b.ux) * b.half_w + Math.abs(b.uz) * b.half_d + BOUNDS_PAD;
    const ez = Math.abs(b.uz) * b.half_w + Math.abs(b.ux) * b.half_d + BOUNDS_PAD;
    add(boxes, i, b.x - ex, b.z - ez, b.x + ex, b.z + ez);
  });
  g.cylinders.forEach((c, i) => {
    const r = c.radius + BOUNDS_PAD;
    add(cylinders, i, c.x - r, c.z - r, c.x + r, c.z + r);
  });
  return { ...g, buckets: { nx, nz, boxes, cylinders } };
}

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
  const seen = new Set<number>();
  for (let iz = iz0; iz <= iz1; iz++) {
    for (let ix = ix0; ix <= ix1; ix++) {
      for (const i of list[iz * s.nx + ix]!) {
        seen.add(i);
      }
    }
  }
  return [...seen].sort((a, b) => a - b);
}
