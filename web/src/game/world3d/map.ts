// A map as the client walks and draws it (3d-world.md section 4), read from the JSON server/cmd/mapjson writes: Go's numbers as they are, so the sim meets the same solids.
import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import { ground } from '@/sim/step';
import type { Box, Cylinder, Geometry } from '@/sim/step';
import { height, terrain } from '@/sim/terrain';

/** How far above or below a portal's ground feet in its circle stand in it, as the server's portalHeight. */
const PORTAL_HEIGHT = 1;

/** A solid as drawn: its collision shape and its kit model; a box's yaw, degrees from north toward east, is the one its axis was built from. */
export type Solid =
  | { readonly kind: 'box'; readonly box: Box; readonly model: string; readonly yaw: number }
  | { readonly kind: 'cylinder'; readonly cylinder: Cylinder; readonly model: string };

/** A look-only model at a spot, turned yaw degrees from north toward east. */
export interface Prop {
  readonly model: string;
  readonly x: number;
  readonly z: number;
  readonly yaw: number;
}

/** A portal's circle, and the map it leads to. */
export interface Portal {
  readonly name: string;
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly to: string;
}

export interface WorldMap {
  readonly id: string;
  readonly ground: string; // art keys
  readonly sky: string;
  readonly geometry: Geometry;
  readonly solids: readonly Solid[];
  readonly props: readonly Prop[];
  readonly portals: readonly Portal[];
}

/** One map's JSON, as mapjson writes it. */
interface MapJson {
  readonly id: string;
  readonly ground: string;
  readonly sky: string;
  readonly geometry: {
    readonly size_x: number;
    readonly size_z: number;
    readonly terrain: {
      readonly cell: number;
      readonly nx: number;
      readonly nz: number;
      readonly heights: readonly number[];
    };
    readonly boxes: readonly Box[] | null;
    readonly cylinders: readonly Cylinder[] | null;
  };
  readonly box_models: readonly { readonly model: string; readonly yaw: number }[];
  readonly cylinder_models: readonly string[];
  readonly props: readonly {
    readonly at?: readonly [number, number];
    readonly yaw?: number;
    readonly model?: string;
    readonly scatter?: {
      readonly model: string;
      readonly area: readonly [number, number, number, number];
      readonly count: number;
      readonly seed: number;
    };
  }[];
  readonly portals: readonly {
    readonly name: string;
    readonly at: readonly [number, number];
    readonly radius: number;
    readonly to: string;
  }[];
}

/** The map with id, from its JSON in the page's maps folder. */
export async function loadMap(id: string): Promise<WorldMap> {
  const res = await fetch(`${import.meta.env.BASE_URL}maps/${id}.json`);
  if (!res.ok) {
    throw new Error(`map ${id}: ${res.status}`);
  }
  return parseMap(await res.json());
}

/** The map json holds; a file that isn't one mapjson wrote throws. */
function parseMap(json: unknown): WorldMap {
  const m = json as MapJson;
  const t = m?.geometry?.terrain;
  if (typeof m?.id !== 'string' || !t || t.heights?.length !== t.nx * t.nz) {
    throw new Error('not a map mapjson wrote');
  }
  const boxes = m.geometry.boxes ?? [];
  const cylinders = m.geometry.cylinders ?? [];
  if (boxes.length !== m.box_models.length || cylinders.length !== m.cylinder_models.length) {
    throw new Error(`map ${m.id}: its models and solids differ in number`);
  }
  const geometry = indexed({
    size_x: m.geometry.size_x,
    size_z: m.geometry.size_z,
    terrain: terrain(t.cell, t.nx, t.nz, t.heights, movement.max_slope),
    boxes,
    cylinders,
  });
  return {
    id: m.id,
    ground: m.ground,
    sky: m.sky,
    geometry,
    solids: [
      ...boxes.map((box, i): Solid => {
        const { model, yaw } = m.box_models[i]!;
        return { kind: 'box', box, model, yaw };
      }),
      ...cylinders.map((cylinder, i): Solid => ({
        kind: 'cylinder',
        cylinder,
        model: m.cylinder_models[i]!,
      })),
    ],
    props: m.props.flatMap(expand),
    portals: m.portals.map((p) => ({
      name: p.name,
      x: p.at[0],
      z: p.at[1],
      radius: p.radius,
      to: p.to,
    })),
  };
}

/** A prop's points: its one spot, or its scatter strewn from its seed over its area, each turned at random. */
function expand(p: MapJson['props'][number]): Prop[] {
  if (p.scatter) {
    const { model, area, count, seed } = p.scatter;
    const [x, z, w, d] = area;
    const roll = mulberry32(seed);
    return Array.from({ length: count }, () => ({
      model,
      x: x + roll() * w,
      z: z + roll() * d,
      yaw: roll() * 360,
    }));
  }
  return p.at && p.model ? [{ model: p.model, x: p.at[0], z: p.at[1], yaw: p.yaw ?? 0 }] : [];
}

/** A small seeded roll from 0 to 1: looks only, so it needn't match the server's. */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** The ground under (x, z) for feet at y, as the server's groundUnder: the terrain, or a solid's top no more than a step above the higher of the two. */
export function groundUnder(map: WorldMap, x: number, z: number, y = -Infinity): number {
  const g = map.geometry;
  return ground(x, z, Math.max(y, height(g.terrain, x, z)) + movement.step, g);
}

/** The portal whose circle holds the feet centre, edge included, within PORTAL_HEIGHT of its ground, as the server's standsIn. */
export function standsIn(
  feet: { readonly x: number; readonly y: number; readonly z: number },
  map: WorldMap,
): Portal | undefined {
  return map.portals.find((p) => {
    const dx = feet.x - p.x;
    const dz = feet.z - p.z;
    return (
      Math.sqrt(dx * dx + dz * dz) <= p.radius &&
      Math.abs(feet.y - groundUnder(map, p.x, p.z)) <= PORTAL_HEIGHT
    );
  });
}
