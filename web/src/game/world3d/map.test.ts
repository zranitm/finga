import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
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

  it('reads the town and forest as Go wrote them, the sim’s geometry number for number', async () => {
    for (const id of ['town', 'forest']) {
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
    const m = await loadMap('forest');
    const flowers = m.props.filter((p) => p.model === 'flowers');
    expect(flowers).toHaveLength(120);
    for (const p of flowers) {
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThan(72);
      expect(p.z).toBeGreaterThanOrEqual(0);
      expect(p.z).toBeLessThan(40);
    }
    expect(new Set(flowers.map((p) => `${p.x} ${p.z}`)).size).toBe(120);
    expect((await loadMap('forest')).props).toEqual(m.props);
    expect(m.props.filter((p) => p.model === 'grass_tuft')).toEqual([
      { model: 'grass_tuft', x: 12, z: 8, yaw: 0 },
    ]);
  });

  it('carries each portal’s target', async () => {
    expect((await loadMap('forest')).portals).toEqual([
      { name: 'from_town', x: 2, z: 20, radius: 1.2, to: 'town' },
      { name: 'to_cave', x: 70, z: 20, radius: 1.2, to: 'cave' },
    ]);
  });

  it('refuses what isn’t a map mapjson wrote', async () => {
    await expect(read({ id: 'x' })).rejects.toThrow();
    await expect(read(null)).rejects.toThrow();
    const raw = json('town') as { box_models: unknown[] };
    await expect(read({ ...raw, box_models: raw.box_models.slice(1) })).rejects.toThrow();
    const region = json('test_region') as {
      geometry: { terrain: { deep_water: boolean[] } };
    };
    const short = {
      ...region.geometry.terrain,
      deep_water: region.geometry.terrain.deep_water.slice(1),
    };
    await expect(
      read({ ...region, geometry: { ...region.geometry, terrain: short } }),
    ).rejects.toThrow('not a map mapjson wrote');
  });

  it('reads the region a map names, and refuses one that names no bake', async () => {
    const raw = json('town') as object;
    expect((await read({ ...raw, region: { id: 'tiny', hash: 'abc' } })).region).toEqual({
      id: 'tiny',
      hash: 'abc',
    });
    expect((await read(raw)).region).toBeNull();
    for (const region of [{ id: 'tiny' }, { hash: 'abc' }, { id: 'tiny', hash: 7 }]) {
      await expect(read({ ...raw, region })).rejects.toThrow('map town: its region names no bake');
    }
  });

  it('reads a region with its solids and deep water for the sim and no models, its bake drawing them; a map still needs a model for each solid', async () => {
    const region = await loadMap('test_region');
    expect(region.region?.id).toBe('test_region');
    expect(region.region?.hash).toMatch(/^[0-9a-f]{16}$/);
    expect(region.geometry.cylinders.length).toBeGreaterThan(100);
    expect(region.geometry.boxes.length).toBeGreaterThan(0);
    expect(region.solids).toEqual([]);
    const raw = json('test_region') as { geometry: { terrain: { deep_water: boolean[] } } };
    expect(Array.from(region.geometry.terrain.deepWater!, Boolean)).toEqual(
      raw.geometry.terrain.deep_water,
    );
    expect(raw.geometry.terrain.deep_water.filter(Boolean).length).toBeGreaterThan(1000);
    expect((await loadMap('town')).geometry.terrain.deepWater).toBeUndefined();
    const town = json('town') as { box_models: unknown[] };
    await expect(read({ ...town, box_models: town.box_models.slice(1) })).rejects.toThrow(
      'map town: its models and solids differ in number',
    );
  });

  it('loads a map by its id from the page’s maps folder, and one not there throws', async () => {
    expect((await loadMap('cave')).sky).toBe('cave');
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
    const forest = await loadMap('forest');
    const p = forest.portals[1]!;
    expect(standsIn({ x: p.x, y: groundUnder(forest, p.x, p.z), z: p.z }, forest)?.to).toBe('cave');
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
