// A chunk fetched and built into arrays a mesh takes as they are: what the chunk workers run, off the page's thread (large-regions.md section 10).
import { chunkGround } from './ground-mesh';
import type { ChunkArrays } from './ground-mesh';
import { fetched, readChunk } from './region-bake';
import type { Chunk } from './region-bake';
import type { ChunkGround } from './region-ground';

/** What a chunk is built from: its file's url, the region's cell and linear palette, and the widest spacing, in samples, a neighbour draws an edge at. */
export interface ChunkRequest {
  readonly url: string;
  readonly cell: number;
  readonly linear: Float32Array;
  readonly coarsest: number;
}

/** A chunk built: its ground, its props as model indices and, four to a prop, x, ground height, z and yaw in degrees, and what the sim takes from it. */
export interface BuiltChunk {
  readonly cx: number;
  readonly cz: number;
  readonly ground: ChunkArrays;
  readonly models: Uint16Array;
  readonly props: Float32Array;
  readonly sim: ChunkGround;
}

/** Builds the chunks a region asks for: on workers in the page, in tests on their own thread. */
export interface ChunkSource {
  build(req: ChunkRequest): Promise<BuiltChunk>;
  /** Ends its workers once the region's left; what they hadn't answered never settles, and a later build starts them again. */
  close(): void;
}

export async function buildChunk(req: ChunkRequest): Promise<BuiltChunk> {
  const c = readChunk(await (await fetched(req.url)).arrayBuffer());
  const count = c.props.models.length;
  const props = new Float32Array(4 * count);
  for (let i = 0; i < count; i++) {
    const [x, z, yaw] = c.props.at.subarray(3 * i, 3 * i + 3);
    props.set([x!, groundAt(c, req.cell, x!, z!), z!, yaw!], 4 * i);
  }
  return {
    cx: c.cx,
    cz: c.cz,
    ground: chunkGround(c, req.cell, req.linear, req.coarsest),
    models: c.props.models,
    props,
    sim: {
      cx: c.cx,
      cz: c.cz,
      n: c.n,
      heights: c.heights.slice(), // its own buffer, not the file's, to hand over
      barrier: c.barrier.slice(),
      cylinders: c.cylinders,
      boxes: c.boxes,
    },
  };
}

/** The buffers a built chunk hands over rather than copies. */
export function transfers(b: BuiltChunk): ArrayBuffer[] {
  const { positions, normals, colors, details } = b.ground;
  const { heights, barrier, cylinders, boxes } = b.sim;
  return [
    positions,
    normals,
    colors,
    ...details,
    b.models,
    b.props,
    heights,
    barrier,
    cylinders.index,
    cylinders.at,
    boxes.index,
    boxes.at,
  ].map((a) => a.buffer as ArrayBuffer);
}

/** The ground's height at (x, z) in the world, bilinear between c's samples as the sim reads the terrain. */
function groundAt(c: Chunk, cell: number, x: number, z: number): number {
  const side = c.n + 2;
  const at = (v: number, origin: number): [number, number] => {
    const g = (v - origin) / cell + 1; // past the border
    const i = Math.min(Math.max(Math.floor(g), 0), side - 2);
    return [i, Math.min(Math.max(g - i, 0), 1)];
  };
  const span = (c.n - 1) * cell;
  const [ix, fx] = at(x, c.cx * span);
  const [iz, fz] = at(z, c.cz * span);
  const h = (dx: number, dz: number) => c.heights[(iz + dz) * side + ix + dx]!;
  const near = h(0, 0) + (h(1, 0) - h(0, 0)) * fx;
  const far = h(0, 1) + (h(1, 1) - h(0, 1)) * fx;
  return near + (far - near) * fz;
}
