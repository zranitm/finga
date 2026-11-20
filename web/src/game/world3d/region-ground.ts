// A region's ground as the client's sim walks it (large-regions.md section 4): built from the bake's chunks as they come, the same numbers the server's sim reads.
import { fileBox, fileCylinder, solidBuckets } from '@/sim/solids';
import type { FilledSolids } from '@/sim/solids';
import type { Box, Cylinder, Geometry } from '@/sim/step';
import { markSteep } from '@/sim/terrain';

import type { Chunk, Region } from './region-bake';

/** What the sim takes from a chunk. */
export type ChunkGround = Pick<
  Chunk,
  'cx' | 'cz' | 'n' | 'heights' | 'deep' | 'cylinders' | 'boxes'
>;

/** One height grid for the whole region, its steep and deep marks, and its solids at the server's indices in its buckets, each chunk's filled as it comes; ground not in yet reads flat at 0, which the world keeps the player off. */
export class RegionGround {
  readonly geometry: Geometry;
  private readonly terrain: {
    readonly cell: number;
    readonly nx: number;
    readonly nz: number;
    readonly heights: Float32Array;
    readonly deepWater: Uint8Array;
    readonly steepX: Uint8Array;
    readonly steepZ: Uint8Array;
  };
  private readonly cylinders: Cylinder[];
  private readonly boxes: Box[];
  private readonly buckets: FilledSolids;
  private readonly limit: number;
  private readonly per: number; // samples along a chunk's side, less its shared edge
  private readonly side: number;
  private readonly ncx: number;
  private readonly ncz: number;
  private readonly filled: Uint8Array; // per chunk, 1 once in

  constructor(region: Region, maxSlope: number) {
    const [sx, sz] = region.size;
    const nx = Math.round(sx / region.cell) + 1;
    const nz = Math.round(sz / region.cell) + 1;
    this.terrain = {
      cell: region.cell,
      nx,
      nz,
      heights: new Float32Array(nx * nz),
      deepWater: new Uint8Array(nx * nz),
      steepX: new Uint8Array((nx - 1) * (nz - 1)),
      steepZ: new Uint8Array((nx - 1) * (nz - 1)),
    };
    this.cylinders = new Array<Cylinder>(region.solids.cylinders);
    this.boxes = new Array<Box>(region.solids.boxes);
    this.buckets = solidBuckets(sx, sz);
    this.limit = maxSlope * region.cell;
    this.per = Math.round(region.chunk / region.cell);
    this.side = region.chunk;
    [this.ncx, this.ncz] = region.chunks;
    this.filled = new Uint8Array(this.ncx * this.ncz);
    this.geometry = {
      size_x: sx,
      size_z: sz,
      terrain: this.terrain,
      boxes: this.boxes,
      cylinders: this.cylinders,
      buckets: this.buckets,
    };
  }

  /** Fills chunk c's samples, the cells between them and the solids it holds; a chunk in already is left. */
  add(c: ChunkGround): void {
    const k = c.cz * this.ncx + c.cx;
    if (this.filled[k]) {
      return;
    }
    const t = this.terrain;
    const x0 = c.cx * this.per;
    const z0 = c.cz * this.per;
    for (let iz = 0; iz < c.n; iz++) {
      for (let ix = 0; ix < c.n; ix++) {
        const s = (z0 + iz) * t.nx + x0 + ix;
        t.heights[s] = c.heights[(iz + 1) * (c.n + 2) + ix + 1]!;
        t.deepWater[s] = c.deep[iz * c.n + ix]!;
      }
    }
    markSteep(t.heights, t.nx, this.limit, t.steepX, t.steepZ, x0, x0 + c.n - 2, z0, z0 + c.n - 2);
    c.cylinders.index.forEach((i, j) => {
      const [x, z, radius, top] = c.cylinders.at.subarray(4 * j, 4 * j + 4);
      this.cylinders[i] = { x: x!, z: z!, radius: radius!, top: top! };
      fileCylinder(this.buckets, i, this.cylinders[i]);
    });
    c.boxes.index.forEach((i, j) => {
      const [x, z, half_w, half_d, ux, uz, top] = c.boxes.at.subarray(7 * j, 7 * j + 7);
      this.boxes[i] = {
        x: x!,
        z: z!,
        half_w: half_w!,
        half_d: half_d!,
        ux: ux!,
        uz: uz!,
        top: top!,
      };
      fileBox(this.buckets, i, this.boxes[i]);
    });
    this.filled[k] = 1;
  }

  /** Whether every chunk within reach metres of (x, z) is in: the ground a body there could read. */
  covers(x: number, z: number, reach: number): boolean {
    const span = (v: number, n: number) => [
      Math.max(Math.floor((v - reach) / this.side), 0),
      Math.min(Math.floor((v + reach) / this.side), n - 1),
    ];
    const [cx0, cx1] = span(x, this.ncx);
    const [cz0, cz1] = span(z, this.ncz);
    for (let cz = cz0!; cz <= cz1!; cz++) {
      for (let cx = cx0!; cx <= cx1!; cx++) {
        if (!this.filled[cz * this.ncx + cx]) {
          return false;
        }
      }
    }
    return true;
  }
}
