// A region's bake as cmd/regionbake writes it (large-regions.md section 3): region.json, the 8 m overview and the 64 m chunks, all little-endian.

/** The bake format this client reads; any other is refused. */
export const BAKE_FORMAT = 3;

/** Which bake of which region: its id, and the hash its folder is named by. */
export interface RegionRef {
  readonly id: string;
  readonly hash: string;
}

/** What the client draws a region by, from region.json. */
export interface Region {
  readonly ref: RegionRef;
  readonly sky: string;
  readonly size: readonly [number, number];
  readonly cell: number;
  readonly chunk: number;
  readonly chunks: readonly [number, number];
  /** How many cylinders and boxes the region has, which its chunks' solids index into. */
  readonly solids: { readonly cylinders: number; readonly boxes: number };
  /** Each ground's colour, 0xrrggbb sRGB, by the index chunks and the overview hold. */
  readonly palette: readonly number[];
  /** Each kit model's key, by the index a chunk's props hold. */
  readonly models: readonly string[];
}

/** The whole region every few samples: nx by nz heights and colour indices, row by row from the south-west, spacing metres apart. */
export interface Overview {
  readonly nx: number;
  readonly nz: number;
  readonly spacing: number;
  readonly heights: Float32Array;
  readonly colours: Uint8Array;
}

/** A chunk's props: model indices, and each one's x, z and yaw in degrees, three to a prop. */
export interface ChunkProps {
  readonly models: Uint16Array;
  readonly at: Float32Array;
}

/** A chunk's solids of one kind: each one's index in the region's list, and its numbers, per of them to a solid. */
export interface ChunkSolids {
  readonly index: Uint32Array;
  readonly at: Float32Array;
}

/** One chunk: n samples a side, its edges shared with its neighbours; (n+2)² heights, a border a sample wide round its own; n² colours and deep-water marks; and the props and solids whose centres it holds, cylinders as x, z, radius and top, boxes as x, z, half width, half depth, ux, uz and top. */
export interface Chunk {
  readonly cx: number;
  readonly cz: number;
  readonly n: number;
  readonly heights: Float32Array;
  readonly colours: Uint8Array;
  readonly deep: Uint8Array;
  readonly props: ChunkProps;
  readonly cylinders: ChunkSolids;
  readonly boxes: ChunkSolids;
}

/** Where a bake's files are served. */
export function bakeUrl(ref: RegionRef, file: string): string {
  return `${import.meta.env.BASE_URL}regions/${ref.id}/${ref.hash}/${file}`;
}

/** A chunk file's name in its bake. */
export function chunkFile(cx: number, cz: number): string {
  return `chunks/${cx}_${cz}.bin`;
}

/** The region ref names and its overview, from its bake. */
export async function loadRegion(ref: RegionRef): Promise<{ region: Region; overview: Overview }> {
  const [meta, overview] = await Promise.all([
    fetched(bakeUrl(ref, 'region.json')).then((r) => r.json() as Promise<RegionJson>),
    fetched(bakeUrl(ref, 'overview.bin')).then((r) => r.arrayBuffer()),
  ]);
  const region = parseRegion(ref, meta);
  return { region, overview: readOverview(overview, region.cell) };
}

/** The response from url, or an error naming it. */
export async function fetched(url: string): Promise<Response> {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`${url}: ${res.status}`);
  }
  return res;
}

interface RegionJson {
  readonly format: number;
  readonly sky: string;
  readonly size: [number, number];
  readonly cell: number;
  readonly chunk: number;
  readonly chunks: [number, number];
  readonly solids: { readonly cylinders: number; readonly boxes: number };
  readonly palette: readonly { readonly name: string; readonly rgb: string }[];
  readonly models: readonly string[];
}

function parseRegion(ref: RegionRef, j: RegionJson): Region {
  if (j.format !== BAKE_FORMAT) {
    throw new Error(`region ${ref.id}: bake format ${j.format}; this client reads ${BAKE_FORMAT}`);
  }
  return {
    ref,
    sky: j.sky,
    size: j.size,
    cell: j.cell,
    chunk: j.chunk,
    chunks: j.chunks,
    solids: j.solids,
    palette: j.palette.map((c) => parseInt(c.rgb.slice(1), 16)),
    models: j.models,
  };
}

/** Reads a file's magic, format and three fields, and refuses another magic or format. */
function header(buf: ArrayBuffer, magic: string): [number, number, number] {
  const v = new DataView(buf);
  const got = String.fromCharCode(...new Uint8Array(buf, 0, 4));
  if (got !== magic || v.getUint16(4, true) !== BAKE_FORMAT) {
    throw new Error(`not a format ${BAKE_FORMAT} ${magic}: ${got} ${v.getUint16(4, true)}`);
  }
  return [v.getUint16(6, true), v.getUint16(8, true), v.getUint16(10, true)];
}

const HEADER = 12;

/** overview.bin, of a region whose samples are cell metres apart: "FROV", its samples along x and z and its step in samples; then its heights, then its colours. */
export function readOverview(buf: ArrayBuffer, cell: number): Overview {
  const [nx, nz, step] = header(buf, 'FROV');
  const heights = new Float32Array(buf, HEADER, nx * nz);
  return {
    nx,
    nz,
    spacing: step * cell,
    heights,
    colours: new Uint8Array(buf, HEADER + 4 * nx * nz, nx * nz),
  };
}

/** A chunk file: "FRCK", its x, z and samples a side; its bordered heights, its colours and its deep-water marks each padded to 4 bytes, then its props, cylinders and boxes. */
export function readChunk(buf: ArrayBuffer): Chunk {
  const [cx, cz, n] = header(buf, 'FRCK');
  const v = new DataView(buf);
  const heights = new Float32Array(buf, HEADER, (n + 2) * (n + 2));
  let at = HEADER + heights.byteLength;
  const colours = new Uint8Array(buf, at, n * n);
  at += (n * n + 3) & ~3;
  const deep = new Uint8Array(buf, at, n * n);
  at += (n * n + 3) & ~3;
  const count = v.getUint32(at, true);
  at += 4;
  const models = new Uint16Array(count);
  const placed = new Float32Array(3 * count);
  for (let i = 0; i < count; i++, at += 16) {
    models[i] = v.getUint16(at, true);
    placed[3 * i] = v.getFloat32(at + 4, true);
    placed[3 * i + 1] = v.getFloat32(at + 8, true);
    placed[3 * i + 2] = v.getFloat32(at + 12, true);
  }
  const solids = (per: number): ChunkSolids => {
    const count = v.getUint32(at, true);
    at += 4;
    const index = new Uint32Array(count);
    const numbers = new Float32Array(per * count);
    for (let i = 0; i < count; i++) {
      index[i] = v.getUint32(at, true);
      for (let k = 0; k < per; k++) {
        numbers[per * i + k] = v.getFloat32(at + 4 + 4 * k, true);
      }
      at += 4 + 4 * per;
    }
    return { index, at: numbers };
  };
  const cylinders = solids(4);
  const boxes = solids(7);
  return { cx, cz, n, heights, colours, deep, props: { models, at: placed }, cylinders, boxes };
}
