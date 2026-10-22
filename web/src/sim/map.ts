// The collision view of a Tiled map (page 5's conventions), parsed from the
// .tmj the server validates in CI. The maps hold only object layers, no
// tile art; the World scene (C07) draws from the same objects.
import * as v from 'valibot';

import type { Geometry, Rect } from './step';

const tiledProp = v.object({ name: v.string(), type: v.string(), value: v.unknown() });
const tiledObject = v.object({
  name: v.string(),
  x: v.number(),
  y: v.number(),
  width: v.number(),
  height: v.number(),
  properties: v.optional(v.array(tiledProp), []),
});
const tiledLayer = v.object({
  name: v.string(),
  type: v.string(),
  objects: v.optional(v.array(tiledObject), []),
});
const tiledMap = v.object({
  type: v.literal('map'),
  width: v.pipe(v.number(), v.integer(), v.minValue(1)),
  height: v.pipe(v.number(), v.integer(), v.minValue(1)),
  tilewidth: v.pipe(v.number(), v.integer(), v.minValue(1)),
  tileheight: v.pipe(v.number(), v.integer(), v.minValue(1)),
  properties: v.optional(v.array(tiledProp), []),
  layers: v.array(tiledLayer),
});

type TiledObject = v.InferOutput<typeof tiledObject>;
type TiledProp = v.InferOutput<typeof tiledProp>;

export type MapKind = 'town' | 'hunting';

export interface Point {
  readonly x: number;
  readonly y: number;
}

/** A portal leads to a named portal on another map; arrival is the bottom centre. */
export interface Portal {
  readonly name: string;
  readonly area: Rect;
  readonly target_map: string;
  readonly target_portal: string;
}

export interface Spawn {
  readonly mob: string;
  readonly max: number;
  readonly respawn_seconds: number;
  readonly area: Rect;
}

export interface MapView {
  readonly id: string;
  readonly kind: MapKind;
  readonly town: string; // the map its players respawn in; a town names itself
  readonly width: number; // pixels
  readonly height: number;
  readonly geometry: Geometry;
  readonly portals: readonly Portal[];
  readonly spawns: readonly Spawn[];
  readonly player_spawn: Point;
}

const requiredLayers = ['solids', 'platforms', 'ropes', 'portals', 'spawns', 'player_spawn'];

export class MapError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'MapError';
  }
}

/** Where a character arriving through the portal stands: the bottom centre. */
export function arrival(p: Portal): Point {
  return { x: p.area.x + p.area.w / 2, y: p.area.y + p.area.h };
}

/** Parses a Tiled map; id is the file name without .tmj. */
export function parseMap(id: string, json: unknown): MapView {
  const parsed = v.safeParse(tiledMap, json);
  if (!parsed.success) {
    throw new MapError(
      `${id}.tmj: not a Tiled map with object layers: ${parsed.issues[0].message}`,
    );
  }
  const tm = parsed.output;
  const kind = propString(tm.properties, 'kind');
  if (kind !== 'town' && kind !== 'hunting') {
    throw new MapError(`${id}.tmj: map property kind must be town or hunting, got ${String(kind)}`);
  }
  let town = id;
  if (kind === 'hunting') {
    const named = propString(tm.properties, 'town');
    if (named === undefined) {
      throw new MapError(`${id}.tmj: a hunting map needs the map property town`);
    }
    town = named;
  }
  const layers = new Map<string, TiledObject[]>();
  for (const layer of tm.layers) {
    if (layer.type === 'objectgroup') {
      layers.set(layer.name, layer.objects);
    }
  }
  for (const name of requiredLayers) {
    if (!layers.has(name)) {
      throw new MapError(`${id}.tmj: missing layer ${name}`);
    }
  }
  const objects = (name: string): TiledObject[] => layers.get(name) ?? [];
  const portals = objects('portals').map((o): Portal => {
    const target_map = propString(o.properties, 'target_map');
    const target_portal = propString(o.properties, 'target_portal');
    if (target_map === undefined || target_portal === undefined) {
      throw new MapError(`${id}.tmj: portal ${o.name} needs target_map and target_portal`);
    }
    return { name: o.name, area: rect(o), target_map, target_portal };
  });
  const spawns = objects('spawns').map((o): Spawn => {
    const mob = propString(o.properties, 'mob');
    const max = propNumber(o.properties, 'max');
    const respawn = propNumber(o.properties, 'respawn_seconds');
    if (mob === undefined || max === undefined || respawn === undefined) {
      throw new MapError(`${id}.tmj: spawn ${o.name} needs mob, max and respawn_seconds`);
    }
    return { mob, max, respawn_seconds: respawn, area: rect(o) };
  });
  const starts = objects('player_spawn');
  const start = starts[0];
  if (starts.length !== 1 || start === undefined) {
    throw new MapError(
      `${id}.tmj: layer player_spawn needs exactly one object, has ${starts.length}`,
    );
  }
  return {
    id,
    kind,
    town,
    width: tm.width * tm.tilewidth,
    height: tm.height * tm.tileheight,
    geometry: {
      solids: objects('solids').map(rect),
      platforms: objects('platforms').map(rect),
      ropes: objects('ropes').map(rect),
    },
    portals,
    spawns,
    player_spawn: { x: start.x + start.width / 2, y: start.y + start.height },
  };
}

function rect(o: TiledObject): Rect {
  return { x: o.x, y: o.y, w: o.width, h: o.height };
}

function propString(props: readonly TiledProp[], name: string): string | undefined {
  const p = props.find((q) => q.name === name);
  return typeof p?.value === 'string' ? p.value : undefined;
}

function propNumber(props: readonly TiledProp[], name: string): number | undefined {
  const p = props.find((q) => q.name === name);
  return typeof p?.value === 'number' ? p.value : undefined;
}
