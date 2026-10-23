import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { arrival, MapError, parseMap } from './map';

const mapsDir = path.resolve(process.cwd(), '..', 'server', 'data', 'maps');

function load(id: string): unknown {
  return JSON.parse(readFileSync(path.join(mapsDir, `${id}.tmj`), 'utf8'));
}

describe('parseMap', () => {
  it.each([
    {
      id: 'town',
      kind: 'town',
      town: 'town',
      solids: 4,
      platforms: 2,
      ropes: 1,
      portals: 1,
      spawns: 0,
    },
    {
      id: 'forest',
      kind: 'hunting',
      town: 'town',
      solids: 4,
      platforms: 3,
      ropes: 2,
      portals: 2,
      spawns: 3,
    },
    {
      id: 'cave',
      kind: 'hunting',
      town: 'town',
      solids: 6,
      platforms: 4,
      ropes: 3,
      portals: 1,
      spawns: 3,
    },
  ])(
    '$id parses into its object counts',
    ({ id, kind, town, solids, platforms, ropes, portals, spawns }) => {
      const m = parseMap(id, load(id));
      expect(m.id).toBe(id);
      expect(m.kind).toBe(kind);
      expect(m.town).toBe(town);
      expect(m.geometry.solids).toHaveLength(solids);
      expect(m.geometry.platforms).toHaveLength(platforms);
      expect(m.geometry.ropes).toHaveLength(ropes);
      expect(m.portals).toHaveLength(portals);
      expect(m.spawns).toHaveLength(spawns);
      expect(m.width).toBeGreaterThan(0);
      expect(m.height).toBeGreaterThan(0);
    },
  );

  it('reads the portal links and arrival points the server uses', () => {
    const town = parseMap('town', load('town'));
    const forest = parseMap('forest', load('forest'));
    const toForest = town.portals.find((p) => p.name === 'to_forest');
    expect(toForest).toMatchObject({ target_map: 'forest', target_portal: 'from_town' });
    expect(arrival(toForest!)).toEqual({ x: 1216, y: 608 });
    const fromTown = forest.portals.find((p) => p.name === 'from_town');
    expect(arrival(fromTown!)).toEqual({ x: 64, y: 608 });
    expect(town.player_spawn).toEqual({ x: 416, y: 608 });
    expect(forest.spawns[0]).toMatchObject({ mob: 'green_slime', max: 5, respawn_seconds: 8 });
  });

  it.each([
    {
      name: 'a missing layer',
      edit: (s: string) => s.replace('"name": "ropes"', '"name": "strings"'),
      want: /missing layer ropes/,
    },
    {
      name: 'an unknown kind',
      edit: (s: string) => s.replace('"value": "town"', '"value": "dungeon"'),
      want: /kind must be/,
    },
    {
      name: 'two player spawns',
      edit: (s: string) => s.replace('"name": "player_spawn"', '"name": "player_spawn_x"'),
      want: /missing layer player_spawn/,
    },
    { name: 'not a map', edit: () => '{"type":"tileset"}', want: /not a Tiled map/ },
  ])('rejects $name', ({ edit, want }) => {
    const json = edit(readFileSync(path.join(mapsDir, 'town.tmj'), 'utf8'));
    expect(() => parseMap('town', JSON.parse(json))).toThrow(MapError);
    expect(() => parseMap('town', JSON.parse(json))).toThrow(want);
  });

  it('a hunting map without a town property is refused', () => {
    const json = readFileSync(path.join(mapsDir, 'forest.tmj'), 'utf8').replace(
      '"name": "town"',
      '"name": "home"',
    );
    expect(() => parseMap('forest', JSON.parse(json))).toThrow(/needs the map property town/);
  });
});
