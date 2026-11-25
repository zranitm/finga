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
  /** The region's lowest and highest ground. */
  readonly heights: readonly [number, number];
  /** How many cylinders and boxes the region has, which its chunks' solids index into, and the farthest any reaches from its centre, so from the chunk that carries it. */
  readonly solids: { readonly cylinders: number; readonly boxes: number; readonly reach: number };
  /** Each ground's colour, 0xrrggbb sRGB, by the index chunks and the overview hold. */
  readonly palette: readonly number[];
  /** Each kit model's key, by the index a chunk's props hold. */
  readonly models: readonly string[];
  /** Its towns and hunting areas, by the ids the server names them by. */
  readonly areas: readonly RegionArea[];
  /** Its water's colour, 0xrrggbb sRGB: the palette's water. */
  readonly water: number;
  readonly rivers: readonly River[];
  readonly lakes: readonly Lake[];
}

/** A river as carved: its smoothed path downstream, x and z, its channel's width, and its water's level at each point. */
export interface River {
  readonly path: readonly (readonly [number, number])[];
  readonly width: number;
  readonly levels: readonly number[];
}

/** A lake: its area's corners, x and z, and its water's level. */
export interface Lake {
  readonly area: readonly (readonly [number, number])[];
  readonly level: number;
}

/** One of a region's areas: a town, or a hunting area of its mobs' levels. */
export interface RegionArea {
  readonly id: string;
  readonly name: string;
  readonly kind: 'town' | 'hunting';
  /** A hunting area's lowest and highest mob levels; 0 and 0 in a town. */
  readonly levels: readonly [number, number];
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

/** One chunk: n samples a side, its edges shared with its neighbours; (n+2)² heights, a border a sample wide round its own; n² colours and barrier marks; and the props and solids whose centres it holds, cylinders as x, z, radius and top, boxes as x, z, half width, half depth, ux, uz and top. */
export interface Chunk {
  readonly cx: number;
  readonly cz: number;
  readonly n: number;
  readonly heights: Float32Array;
  readonly colours: Uint8Array;
  readonly barrier: Uint8Array;
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
  readonly heights: [number, number];
  readonly solids: { readonly cylinders: number; readonly boxes: number; readonly reach: number };
  readonly palette: readonly { readonly name: string; readonly rgb: string }[];
  readonly models: readonly string[];
  readonly areas: readonly RegionArea[];
  readonly rivers: readonly River[] | null;
  readonly lakes: readonly Lake[] | null;
}

function parseRegion(ref: RegionRef, j: RegionJson): Region {
  if (j.format !== BAKE_FORMAT) {
    throw new Error(`region ${ref.id}: bake format ${j.format}; this client reads ${BAKE_FORMAT}`);
  }
  const water = j.palette.find((c) => c.name === 'water');
  if (!water) {
    throw new Error(`region ${ref.id}: its palette has no water`); // a plan's kit must have it (large-regions.md section 3)
  }
  return {
    ref,
    sky: j.sky,
    size: j.size,
    cell: j.cell,
    chunk: j.chunk,
    chunks: j.chunks,
    heights: j.heights,
    solids: j.solids,
    palette: j.palette.map((c) => parseInt(c.rgb.slice(1), 16)),
    models: j.models,
    areas: j.areas.map(({ id, name, kind, levels }) => ({ id, name, kind, levels })),
    water: parseInt(water.rgb.slice(1), 16),
    rivers: (j.rivers ?? []).map(({ path, width, levels }) => ({ path, width, levels })),
    lakes: (j.lakes ?? []).map(({ area, level }) => ({ area, level })),
  };
}

/** Reads a file's magic, format and three fields, and refuses another magic or format. */
function header(buf: ArrayBuffer, magic: string): [number, number, number] {
  if (buf.byteLength < HEADER) {
    throw new Error(`not a format ${BAKE_FORMAT} ${magic}: ${buf.byteLength} bytes`);
  }
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

/** A chunk file: "FRCK", its x, z and samples a side; its bordered heights, its colours and its barrier marks each padded to 4 bytes, then its props, cylinders and boxes; one that ends early or runs on past them throws. */
export function readChunk(buf: ArrayBuffer): Chunk {
  const [cx, cz, n] = header(buf, 'FRCK');
  const v = new DataView(buf);
  let at = HEADER;
  /** The offset of the next bytes, taken. */
  const take = (bytes: number): number => {
    if (at + bytes > buf.byteLength) {
      throw new Error(`chunk ${cx}_${cz} ends ${at + bytes - buf.byteLength} bytes early`);
    }
    at += bytes;
    return at - bytes;
  };
  const heights = new Float32Array(buf, take(4 * (n + 2) ** 2), (n + 2) ** 2);
  const colours = new Uint8Array(buf, take((n * n + 3) & ~3), n * n);
  const barrier = new Uint8Array(buf, take((n * n + 3) & ~3), n * n);
  const count = v.getUint32(take(4), true);
  const models = new Uint16Array(count);
  const placed = new Float32Array(3 * count);
  for (let i = 0; i < count; i++) {
    const p = take(16);
    models[i] = v.getUint16(p, true);
    placed[3 * i] = v.getFloat32(p + 4, true);
    placed[3 * i + 1] = v.getFloat32(p + 8, true);
    placed[3 * i + 2] = v.getFloat32(p + 12, true);
  }
  const solids = (per: number): ChunkSolids => {
    const count = v.getUint32(take(4), true);
    const index = new Uint32Array(count);
    const numbers = new Float32Array(per * count);
    for (let i = 0; i < count; i++) {
      const p = take(4 + 4 * per);
      index[i] = v.getUint32(p, true);
      for (let k = 0; k < per; k++) {
        numbers[per * i + k] = v.getFloat32(p + 4 + 4 * k, true);
      }
    }
    return { index, at: numbers };
  };
  const cylinders = solids(4);
  const boxes = solids(7);
  if (at !== buf.byteLength) {
    throw new Error(`chunk ${cx}_${cz} runs ${buf.byteLength - at} bytes past its end`);
  }
  return { cx, cz, n, heights, colours, barrier, props: { models, at: placed }, cylinders, boxes };
}
