// A map as the client walks and draws it (3d-world.md section 4), read from the JSON server/cmd/mapjson writes: Go's numbers as they are, so the sim meets the same solids.
import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import { ground } from '@/sim/step';
import type { Box, Cylinder, Geometry } from '@/sim/step';
import { height, terrain } from '@/sim/terrain';
import type { MapWaypoint } from '@/state/map-view';

import { loadRegion } from './region-bake';
import type { Overview, Region, RegionRef } from './region-bake';
import { RegionGround } from './region-ground';

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
  /** A match's map's prep wall: its boxes as drawn, and the map's geometry with them added after its own, as the world walks it through prep and the count into a round (pvp.md section 3); none elsewhere. */
  readonly prepWall: { readonly boxes: readonly Box[]; readonly walled: Geometry } | null;
  /** The region whose bake draws it and holds its ground, or null for a map drawn whole from its own JSON. */
  readonly region: RegionMap | null;
  /** A region's waypoints; none elsewhere. */
  readonly waypoints: readonly MapWaypoint[];
}

/** A region as the world reads it from its bake: what draws it, and the ground its sim walks, which its geometry is, filled as its chunks come (large-regions.md section 10). */
export interface RegionMap {
  readonly ref: RegionRef;
  readonly region: Region;
  readonly overview: Overview;
  readonly ground: RegionGround;
}

/** One map's JSON, as mapjson writes it. */
interface MapJson {
  readonly id: string;
  readonly ground: string;
  readonly sky: string;
  readonly geometry?: {
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
  readonly prep_wall?: {
    readonly boxes: readonly Box[] | null;
    readonly cylinders: readonly Cylinder[] | null;
  };
  readonly region?: RegionRef;
  readonly waypoints?: readonly {
    readonly id: string;
    readonly name: string;
    readonly at: readonly [number, number];
    readonly main: boolean;
  }[];
}

/** The map with id, from its JSON in the page's maps folder. */
export async function loadMap(id: string): Promise<WorldMap> {
  const res = await fetch(`${import.meta.env.BASE_URL}maps/${id}.json`);
  if (!res.ok) {
    throw new Error(`map ${id}: ${res.status}`);
  }
  const json = (await res.json()) as MapJson;
  const ref = json?.region;
  if (ref === undefined) {
    return parseMap(json, null);
  }
  if (typeof ref?.id !== 'string' || typeof ref.hash !== 'string') {
    throw new Error(`map ${String(json.id)}: its region names no bake`);
  }
  const { region, overview } = await loadRegion(ref);
  return parseMap(json, {
    ref,
    region,
    overview,
    ground: new RegionGround(region, movement.max_slope),
  });
}

/** The map m holds, its ground region's if it names one; a file that isn't one mapjson wrote throws. */
function parseMap(m: MapJson, region: RegionMap | null): WorldMap {
  const t = m?.geometry?.terrain;
  if (typeof m?.id !== 'string' || (!region && (!t || t.heights?.length !== t.nx * t.nz))) {
    throw new Error('not a map mapjson wrote');
  }
  const boxes = m.geometry?.boxes ?? [];
  const cylinders = m.geometry?.cylinders ?? [];
  if (
    !region &&
    (boxes.length !== m.box_models.length || cylinders.length !== m.cylinder_models.length)
  ) {
    throw new Error(`map ${m.id}: its models and solids differ in number`);
  }
  const geometry =
    region?.ground.geometry ??
    indexed({
      size_x: m.geometry!.size_x,
      size_z: m.geometry!.size_z,
      terrain: terrain(t!.cell, t!.nx, t!.nz, t!.heights, movement.max_slope),
      boxes,
      cylinders,
    });
  return {
    id: m.id,
    ground: m.ground,
    sky: m.sky,
    geometry,
    solids: region // a region's bake draws its solids
      ? []
      : [
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
    prepWall: m.prep_wall
      ? {
          boxes: m.prep_wall.boxes ?? [],
          walled: indexed({
            size_x: geometry.size_x,
            size_z: geometry.size_z,
            terrain: geometry.terrain,
            boxes: [...boxes, ...(m.prep_wall.boxes ?? [])],
            cylinders: [...cylinders, ...(m.prep_wall.cylinders ?? [])],
          }), // its own index: the map's never saw the wall
        }
      : null,
    region,
    waypoints: (m.waypoints ?? []).map((w) => ({
      id: w.id,
      name: w.name,
      x: w.at[0],
      z: w.at[1],
      main: w.main,
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
