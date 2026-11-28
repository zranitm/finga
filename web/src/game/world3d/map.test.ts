import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { movementParams } from '@/game/systems/prediction';
import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import { ground, step } from '@/sim/step';
import type { Geometry, State } from '@/sim/step';
import { mapJson as json, servePublic } from '@/testing/public-on-disk';

import { groundUnder, loadMap, standsIn } from './map';

/** The map raw is, as loadMap reads it. */
function read(raw: unknown) {
  servePublic({ any: raw });
  return loadMap('any');
}

beforeEach(() => servePublic());
afterEach(() => vi.unstubAllGlobals());

/** A 10 m flat square with a portal at (5, 5), radius 1, standing on the solid there. */
function onSolid(top: number) {
  return read({
    id: 'square',
    ground: 'grass',
    sky: 'day',
    geometry: {
      size_x: 10,
      size_z: 10,
      terrain: { cell: 5, nx: 3, nz: 3, heights: [0, 0, 0, 0, 0, 0, 0, 0, 0] },
      boxes: [{ x: 5, z: 5, half_w: 2, half_d: 2, ux: 1, uz: 0, top }],
      cylinders: null,
    },
    box_models: [{ model: 'crate', yaw: 0 }],
    cylinder_models: [],
    props: [],
    portals: [{ name: 'out', at: [5, 5], radius: 1, to: 'town' }],
  });
}

describe('loadMap', () => {
  it('reads a match map’s prep wall: its boxes, and the map’s solids with the wall’s added after them, as the world walks it before a round; other maps have none', async () => {
    const square = await onSolid(1);
    expect(square.prepWall).toBeNull();
    const wall = { x: 5, z: 8, half_w: 4, half_d: 0.3, ux: 1, uz: 0, top: 3 };
    const post = { x: 1, z: 8, radius: 0.5, top: 3 };
    const raw = json('ring') as Record<string, unknown>;
    const m = await read({
      ...raw,
      geometry: { ...(raw.geometry as object), boxes: [{ ...wall, z: 2 }] },
      box_models: [{ model: 'crate', yaw: 0 }],
      prep_wall: { boxes: [wall], cylinders: [post], box_models: [], cylinder_models: [] },
    });
    expect(m.prepWall?.boxes).toEqual([wall]);
    expect(m.prepWall?.walled.boxes).toEqual([{ ...wall, z: 2 }, wall]);
    expect(m.prepWall?.walled.cylinders).toEqual([...m.geometry.cylinders, post]);
    expect(m.prepWall?.walled.terrain).toBe(m.geometry.terrain);
    expect(m.geometry.boxes).toEqual([{ ...wall, z: 2 }]); // the map's own stay as they were
  });

  it('reads a battle map’s bases and points, and each side’s ground, the other side’s base standing after the map’s solids: a body runs on into its own base and stops at the other’s, as the world walks it through the fight', async () => {
    expect((await onSolid(1)).battle).toBeNull();
    const m = await read(json('battlefield'));
    const b = m.battle!;
    expect(b.points.map((p) => [p.id, p.x, p.z, p.radius])).toEqual([
      ['west', 38, 45, 5],
      ['middle', 70, 45, 5],
      ['east', 102, 45, 5],
    ]);
    expect(b.bases).toEqual([
      { x: 13, z: 45, radius: 9 },
      { x: 127, z: 45, radius: 9 },
    ]);
    for (const s of [0, 1] as const) {
      const other = b.bases[s === 0 ? 1 : 0];
      expect(b.kept[s].cylinders).toEqual([
        ...m.geometry.cylinders,
        { x: other.x, z: other.z, radius: other.radius, top: 1000 },
      ]);
      expect(b.kept[s].boxes).toEqual(m.geometry.boxes);
    }
    const p = movementParams('hunting', 1);
    const east = { left: false, right: true, up: false, down: false, jump: false };
    const run = (g: Geometry) => {
      let s: State = {
        x: 112,
        y: ground(112, 45, -Infinity, g),
        z: 45,
        vx: 0,
        vy: 0,
        vz: 0,
        on_ground: true,
        hx: 1,
        hz: 0,
      };
      for (let k = 0; k < 240; k++) {
        s = step(s, east, g, p);
      }
      return s.x;
    };
    const edge = b.bases[1].x - b.bases[1].radius - movement.radius;
    expect(run(b.kept[0])).toBeCloseTo(edge, 2); // side 1 stops at side 2's base
    expect(run(b.kept[1])).toBeGreaterThan(b.bases[1].x); // side 2 runs on into its own
  });

  it('reads the crossroads and the frost trail as Go wrote them, the sim’s geometry number for number', async () => {
    for (const id of ['crossroads', 'frost_trail']) {
      const raw = json(id) as {
        geometry: {
          size_x: number;
          size_z: number;
          terrain: { cell: number; nx: number; nz: number; heights: number[] };
          boxes: unknown[];
          cylinders: unknown[];
        };
        box_models: { model: string; yaw: number }[];
        cylinder_models: string[];
      };
      const m = await read(raw);
      const g = raw.geometry;
      expect(m.id).toBe(id);
      expect([m.geometry.size_x, m.geometry.size_z]).toEqual([g.size_x, g.size_z]);
      expect(m.geometry.terrain.heights).toEqual(g.terrain.heights);
      expect(m.geometry.terrain.steepX).toHaveLength((g.terrain.nx - 1) * (g.terrain.nz - 1));
      expect(m.geometry.boxes).toEqual(g.boxes);
      expect(m.geometry.cylinders).toEqual(g.cylinders);
      const { buckets, ...unbucketed } = m.geometry;
      expect(buckets).toEqual(indexed(unbucketed).buckets); // bucketed, as the server's maps are
      expect(m.solids.map((s) => s.model)).toEqual([
        ...raw.box_models.map((b) => b.model),
        ...raw.cylinder_models,
      ]);
      expect(m.solids.flatMap((s) => (s.kind === 'box' ? [s.yaw] : []))).toEqual(
        raw.box_models.map((b) => b.yaw),
      );
    }
  });

  it('strews a scatter’s count over its area, the same way each time', async () => {
    const m = await loadMap('frost_trail');
    const pebbles = m.props.filter((p) => p.model === 'pebbles');
    expect(pebbles).toHaveLength(40);
    for (const p of pebbles) {
      expect(p.x).toBeGreaterThanOrEqual(3);
      expect(p.x).toBeLessThan(77);
      expect(p.z).toBeGreaterThanOrEqual(5);
      expect(p.z).toBeLessThan(30);
    }
    expect(new Set(pebbles.map((p) => `${p.x} ${p.z}`)).size).toBe(40);
    expect((await loadMap('frost_trail')).props).toEqual(m.props);
    expect(m.props).toContainEqual({ model: 'crag', x: 28, z: 33.5, yaw: 0 }); // a placed one beside the scatter's
  });

  it('carries each portal’s target', async () => {
    expect((await loadMap('frost_trail')).portals).toEqual([
      { name: 'from_crossroads', x: 2, z: 18, radius: 1.2, to: 'crossroads' },
    ]);
  });

  it('carries a region’s waypoints as Go wrote them, the ground’s height at each and its main town’s marked, and none for a map', async () => {
    const { waypoints } = await loadMap('test_region');
    expect(waypoints.map((w) => [w.id, w.main])).toEqual([
      ['town', true],
      ['west', false],
      ['north', false],
      ['woods', false],
    ]);
    const ground = (json('test_region') as { waypoints: { ground: number }[] }).waypoints[1]!
      .ground;
    expect(waypoints[1]).toEqual({
      id: 'west',
      name: 'West Field',
      x: 60,
      z: 85,
      y: ground,
      main: false,
    });
    expect((await loadMap('frost_trail')).waypoints).toEqual([]);
  });

  it('refuses what isn’t a map mapjson wrote', async () => {
    await expect(read({ id: 'x' })).rejects.toThrow();
    await expect(read(null)).rejects.toThrow();
    const raw = json('crossroads') as { box_models: unknown[] };
    await expect(read({ ...raw, box_models: raw.box_models.slice(1) })).rejects.toThrow();
  });

  it('reads the region a map names from its bake, and refuses one that names no bake', async () => {
    const raw = json('crossroads') as { id: string; portals: unknown[] };
    const tiny = await read({
      id: 'tiny',
      sky: 'day',
      portals: raw.portals,
      props: [],
      region: { id: 'tiny', hash: 'abc' },
    });
    expect(tiny.region?.ref).toEqual({ id: 'tiny', hash: 'abc' });
    expect(tiny.region?.region.solids).toEqual({
      cylinders: 90, // one its waypoint's stone
      boxes: 2,
      reach: Math.hypot(3, 3.5),
    });
    expect(tiny.region?.overview.nx).toBe(17);
    expect((await read(raw)).region).toBeNull();
    for (const region of [{ id: 'tiny' }, { hash: 'abc' }, { id: 'tiny', hash: 7 }, null]) {
      await expect(read({ ...raw, region })).rejects.toThrow(
        'map crossroads: its region names no bake',
      );
    }
  });

  it('walks a region on its ground from the bake, empty till its chunks come, and draws no models of its own; a map still needs a model for each solid', async () => {
    const region = await loadMap('test_region');
    expect(region.region?.ref.id).toBe('test_region');
    expect(region.region?.ref.hash).toMatch(/^[0-9a-f]{16}$/);
    expect(region.geometry).toBe(region.region?.ground.geometry);
    expect([region.geometry.size_x, region.geometry.size_z]).toEqual(region.region?.region.size);
    expect(region.geometry.cylinders).toHaveLength(region.region!.region.solids.cylinders);
    expect(Object.keys(region.geometry.cylinders)).toEqual([]); // till its chunks come
    expect(region.solids).toEqual([]);
    expect((json('test_region') as { geometry?: unknown }).geometry).toBeUndefined();
    const crossroads = json('crossroads') as { box_models: unknown[] };
    await expect(
      read({ ...crossroads, box_models: crossroads.box_models.slice(1) }),
    ).rejects.toThrow('map crossroads: its models and solids differ in number');
  });

  it('loads a map by its id from the page’s maps folder, and one not there throws', async () => {
    expect((await loadMap('greyfang_cave')).sky).toBe('cave');
    await expect(loadMap('nowhere')).rejects.toThrow('map nowhere: 404');
  });
});

describe('the ground under a point', () => {
  it('is a solid’s top a step above the terrain, else the terrain', async () => {
    expect(groundUnder(await onSolid(movement.step), 5, 5)).toBe(movement.step);
    expect(groundUnder(await onSolid(0.5), 5, 5)).toBe(0);
    expect(groundUnder(await onSolid(0.5), 8, 8)).toBe(0);
  });

  it('is a taller solid’s top for feet already on it', async () => {
    expect(groundUnder(await onSolid(2), 5, 5, 2)).toBe(2);
    expect(groundUnder(await onSolid(2), 5, 5, 1.5)).toBe(0);
  });
});

describe('standsIn', () => {
  it('finds a portal whose circle holds the feet, its edge included', async () => {
    const m = await onSolid(movement.step);
    const y = movement.step;
    expect(standsIn({ x: 6, y, z: 5 }, m)?.name).toBe('out');
    expect(standsIn({ x: 5, y, z: 4 }, m)?.name).toBe('out');
    expect(standsIn({ x: 6 + 1e-9, y, z: 5 }, m)).toBeUndefined();
    const trail = await loadMap('frost_trail');
    const p = trail.portals[0]!;
    expect(standsIn({ x: p.x, y: groundUnder(trail, p.x, p.z), z: p.z }, trail)?.to).toBe(
      'icefall',
    );
  });

  it('holds feet within 1 m of the portal’s ground, a low solid’s top under it', async () => {
    const m = await onSolid(movement.step);
    const top = movement.step;
    expect(standsIn({ x: 5, y: top + 1, z: 5 }, m)?.to).toBe('town');
    expect(standsIn({ x: 5, y: top - 1, z: 5 }, m)?.to).toBe('town');
    expect(standsIn({ x: 5, y: top + 1.001, z: 5 }, m)).toBeUndefined();
    expect(standsIn({ x: 5, y: 1.001, z: 5 }, await onSolid(0.5))).toBeUndefined(); // a tall solid's top isn't the portal's ground
  });
});
