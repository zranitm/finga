// A region's ground as vertex arrays (large-regions.md section 10): a chunk at three details with skirts, and the overview with holes where chunks are drawn.
import type { Chunk, Overview } from './region-bake';

/** A chunk's details: every sample, every 2nd and every 4th. */
export const DETAILS = [1, 2, 4] as const;

/** A skirt hangs this far below the furthest an edge strays from its neighbour's. */
const SKIRT_PAD = 0.5;

/** A mesh's arrays: positions, normals and linear rgba colours, and its triangles at each detail, or one set. */
export interface GroundArrays {
  readonly positions: Float32Array;
  readonly normals: Float32Array;
  readonly colors: Float32Array;
  readonly details: readonly Uint32Array[];
}

const GAMMA = 2.2; // as Babylon's toLinearSpace, which the scene's other colours go through

/** Each palette colour, sRGB 0xrrggbb, as linear rgb, three to a colour: what the vertex colours index. */
export function linearPalette(palette: readonly number[]): Float32Array {
  const out = new Float32Array(3 * palette.length);
  palette.forEach((rgb, i) => {
    [16, 8, 0].forEach((shift, k) => {
      out[3 * i + k] = Math.pow(((rgb >> shift) & 0xff) / 255, GAMMA);
    });
  });
  return out;
}

/**
 * A chunk's ground, cell metres a sample, from its south-west corner: its n² samples, then a skirt's foot under each edge sample, south, north, west and east.
 * Normals take central differences over the bordered heights, so a shared edge gets the same normal from both chunks. Each detail's triangles carry their own skirts, which hang below where a neighbour drawn every coarsest samples could put the edge.
 */
export function chunkGround(
  c: Chunk,
  cell: number,
  linear: Float32Array,
  coarsest: number,
): GroundArrays {
  const { n } = c;
  const side = n + 2;
  const h = (ix: number, iz: number) => c.heights[(iz + 1) * side + ix + 1]!;
  const count = n * n + 4 * n;
  const positions = new Float32Array(3 * count);
  const normals = new Float32Array(3 * count);
  const colors = new Float32Array(4 * count);
  for (let iz = 0; iz < n; iz++) {
    for (let ix = 0; ix < n; ix++) {
      const v = iz * n + ix;
      positions.set([ix * cell, h(ix, iz), iz * cell], 3 * v);
      normals.set(
        unit(h(ix - 1, iz) - h(ix + 1, iz), 2 * cell, h(ix, iz - 1) - h(ix, iz + 1)),
        3 * v,
      );
      const k = 3 * c.colours[v]!;
      colors.set([linear[k]!, linear[k + 1]!, linear[k + 2]!, 1], 4 * v);
    }
  }
  const edges = edgesOf(n);
  const depth = skirtDepth(edges, positions, coarsest);
  edges.forEach((edge, e) => {
    edge.forEach((v, k) => {
      const foot = n * n + e * n + k;
      positions.set(
        [positions[3 * v]!, positions[3 * v + 1]! - depth, positions[3 * v + 2]!],
        3 * foot,
      );
      normals.set(normals.subarray(3 * v, 3 * v + 3), 3 * foot);
      colors.set(colors.subarray(4 * v, 4 * v + 4), 4 * foot);
    });
  });
  const details = DETAILS.map((s) => {
    const out: number[] = [];
    grid(n, n, s, out);
    edges.forEach((edge, e) => {
      for (let k = 0; k + s < n; k += s) {
        const [a, b] = [edge[k]!, edge[k + s]!];
        const [fa, fb] = [n * n + e * n + k, n * n + e * n + k + s];
        out.push(...(DOWN_FIRST[e] ? [a, fa, b, b, fa, fb] : [a, b, fa, b, fb, fa]));
      }
    });
    return Uint32Array.from(out);
  });
  return { positions, normals, colors, details };
}

/** For each edge, south, north, west and east, whether a skirt's triangle goes down before along to face out of the chunk. */
const DOWN_FIRST = [true, false, false, true] as const;

/** A chunk's edge samples, each edge from its west or south end: south, north, west and east. */
function edgesOf(n: number): number[][] {
  const along = Array.from({ length: n }, (_, k) => k);
  return [
    along.map((k) => k),
    along.map((k) => (n - 1) * n + k),
    along.map((k) => k * n),
    along.map((k) => k * n + n - 1),
  ];
}

/** How far a skirt hangs: the most any edge sample strays from the line its neighbour draws every coarsest samples, and SKIRT_PAD more. */
function skirtDepth(edges: readonly number[][], positions: Float32Array, coarsest: number): number {
  let most = 0;
  const y = (v: number) => positions[3 * v + 1]!;
  for (const edge of edges) {
    for (let k = 0; k + coarsest < edge.length; k += coarsest) {
      const [a, b] = [y(edge[k]!), y(edge[k + coarsest]!)];
      for (let j = 1; j < coarsest; j++) {
        most = Math.max(most, Math.abs(y(edge[k + j]!) - (a + ((b - a) * j) / coarsest)));
      }
    }
  }
  return most + SKIRT_PAD;
}

/** Appends the triangles of an nx by nz grid of vertices, every s samples, clockwise from above as Babylon's front faces are; with keep, only the cells it keeps. */
function grid(
  nx: number,
  nz: number,
  s: number,
  out: number[],
  keep: (ix: number, iz: number) => boolean = () => true,
): void {
  for (let iz = 0; iz + s < nz; iz += s) {
    for (let ix = 0; ix + s < nx; ix += s) {
      if (keep(ix, iz)) {
        const a = iz * nx + ix;
        const c = a + s * nx;
        out.push(a, a + s, c, a + s, c + s, c);
      }
    }
  }
}

/** The overview's ground in the world: its samples spacing apart, normals by central differences held to the region at its edges. */
export function overviewGround(o: Overview, linear: Float32Array): Omit<GroundArrays, 'details'> {
  const count = o.nx * o.nz;
  const h = (ix: number, iz: number) =>
    o.heights[Math.min(Math.max(iz, 0), o.nz - 1) * o.nx + Math.min(Math.max(ix, 0), o.nx - 1)]!;
  const positions = new Float32Array(3 * count);
  const normals = new Float32Array(3 * count);
  const colors = new Float32Array(4 * count);
  for (let iz = 0; iz < o.nz; iz++) {
    for (let ix = 0; ix < o.nx; ix++) {
      const v = iz * o.nx + ix;
      positions.set([ix * o.spacing, h(ix, iz), iz * o.spacing], 3 * v);
      normals.set(
        unit(h(ix - 1, iz) - h(ix + 1, iz), 2 * o.spacing, h(ix, iz - 1) - h(ix, iz + 1)),
        3 * v,
      );
      const k = 3 * o.colours[v]!;
      colors.set([linear[k]!, linear[k + 1]!, linear[k + 2]!, 1], 4 * v);
    }
  }
  return { positions, normals, colors };
}

/** The overview's triangles but those under the chunks drawn, each chunk per cells of the overview a side. */
export function overviewIndices(
  o: Overview,
  per: number,
  drawn: (cx: number, cz: number) => boolean,
): Uint32Array {
  const out: number[] = [];
  grid(o.nx, o.nz, 1, out, (ix, iz) => !drawn(Math.floor(ix / per), Math.floor(iz / per)));
  return Uint32Array.from(out);
}

function unit(x: number, y: number, z: number): [number, number, number] {
  const l = Math.hypot(x, y, z);
  return [x / l, y / l, z / l];
}
