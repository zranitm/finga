import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { maps } from '@/gamedata/maps';
import { parseMap } from '@/sim/map';

import { NAME_TAG_ABOVE, NAME_TAG_TOP, stackTags } from '../entities/name-tag';
import type { TagBox } from '../entities/name-tag';
import { TEX } from '../keys';

import {
  bannerLines,
  belowFloor,
  groundStyle,
  mapTiles,
  platformTiles,
  portalLabel,
  portalLabelY,
  portalTiles,
  ropeTiles,
  solidTiles,
} from './world-layout';

const town = parseMap(
  'town',
  JSON.parse(readFileSync(path.resolve(process.cwd(), 'public/maps/town.tmj'), 'utf8')),
);
const dirt = groundStyle('town');

describe('world layout', () => {
  it.each([
    { map: 'town', top: TEX.dirtMid, background: TEX.bgLand },
    { map: 'forest', top: TEX.grassMid, background: TEX.bgGrass },
    { map: 'cave', top: TEX.stoneMid, background: TEX.bgCave },
  ])('$map looks like $top', ({ map, top, background }) => {
    expect(groundStyle(map)).toMatchObject({ top, background });
  });

  it.each(maps.map((m) => ({ id: m.id })))(
    'map $id has a ground and a background style',
    ({ id }) => {
      const s = groundStyle(id);
      for (const key of [s.top, s.fill, s.platform, s.background]) {
        expect(Object.values(TEX)).toContain(key);
      }
    },
  );

  it.each([
    {
      map: 'shroom_grove',
      ground: [TEX.grassMid, TEX.grassCenter, TEX.grassHalf],
      background: TEX.bgShroom,
      backgroundTint: null,
    },
    {
      map: 'dune_road',
      ground: [TEX.sandMid, TEX.sandCenter, TEX.sandHalf],
      background: TEX.bgDesert,
      backgroundTint: null,
    },
    {
      map: 'frost_trail',
      ground: [TEX.snowMid, TEX.snowCenter, TEX.snowHalf],
      background: TEX.bgCave,
      backgroundTint: 0xc8dcf2,
    },
    {
      map: 'fallen_keep',
      ground: [TEX.stoneMid, TEX.stoneCenter, TEX.stoneHalf],
      tint: 0x8c7f9e,
      background: TEX.bgLand,
      backgroundTint: 0x8a7aa8,
    },
    {
      map: 'crossroads',
      ground: [TEX.dirtMid, TEX.dirtCenter, TEX.dirtHalf],
      background: TEX.bgLand,
      backgroundTint: 0xffd8b0,
    },
  ])(
    '$map draws its region art',
    ({ map, ground: [top, fill, platform], tint = null, background, backgroundTint }) => {
      expect(groundStyle(map)).toEqual({ top, fill, platform, tint, background, backgroundTint });
    },
  );

  it("the keep's tinted ground tints its solids and platforms, not its ropes or portals", () => {
    const keep = groundStyle('fallen_keep');
    const rect = { x: 0, y: 0, w: 64, h: 64 };
    for (const t of [...solidTiles([rect], keep), ...platformTiles([rect], keep)]) {
      expect(t.tint).toBe(0x8c7f9e);
    }
    for (const t of [...ropeTiles([rect]), ...portalTiles(town.portals)]) {
      expect(t.tint).toBeNull();
    }
  });

  it('a map missing from the data is an error, not the forest', () => {
    expect(() => groundStyle('somewhere_new')).toThrow(/somewhere_new/);
  });

  it.each([
    { map: 'town', want: ['Town'] },
    { map: 'forest', want: ['Forest', 'Lv 1\u20134'] },
    { map: 'cave', want: ['Cave', 'Lv 10\u201314'] },
  ])('$map banner reads $want', ({ map, want }) => {
    expect(bannerLines(map)).toEqual(want);
  });

  it.each([
    { target: 'dune_road', want: 'Dune Road · Lv 21\u201322' },
    { target: 'crossroads', want: 'Crossroads' },
  ])('a portal to $target is labelled $want', ({ target, want }) => {
    expect(portalLabel(target)).toBe(want);
  });

  it('every portal leads to a map the client can label', () => {
    for (const m of maps) {
      const view = parseMap(
        m.id,
        JSON.parse(readFileSync(path.resolve(process.cwd(), `public/maps/${m.id}.tmj`), 'utf8')),
      );
      for (const p of view.portals) {
        expect(portalLabel(p.target_map), `${m.id} ${p.name}`).not.toBe('');
      }
    }
  });

  it.each([
    { map: 'town', w: 2560, h: 800 },
    { map: 'dune_road', w: 2560, h: 576 },
  ])(
    "$map in a 2560×1440 window has its ground's fill under the floor, out to the window's edges",
    ({ map, w, h }) => {
      const view = parseMap(
        map,
        JSON.parse(readFileSync(path.resolve(process.cwd(), `public/maps/${map}.tmj`), 'utf8')),
      );
      const { fill, tint } = groundStyle(map);
      expect(belowFloor(view, { width: 2560, height: 1440 })).toEqual({
        texture: fill,
        x: 0,
        y: view.height,
        w,
        h,
        tint,
      });
    },
  );

  it('a window no taller than the map needs nothing under the floor; a narrow one fills the map wide', () => {
    expect(belowFloor(town, { width: 2560, height: 640 })).toBeNull();
    expect(belowFloor(town, { width: 800, height: 900 })).toMatchObject({ w: 1280, h: 260 });
  });

  it("a second player in a door has its tag lifted above the door's label, not onto it", () => {
    const door = town.portals.find((p) => p.target_map === 'forest');
    if (!door) {
      throw new Error('town has no door to the forest');
    }
    const mid = door.area.x + door.area.w / 2;
    const h = NAME_TAG_TOP - NAME_TAG_ABOVE;
    const label: TagBox = { x: mid, bottom: portalLabelY(door.area), w: 100, h };
    const tag: TagBox = { x: mid, bottom: door.area.y + door.area.h - NAME_TAG_ABOVE, w: 60, h };
    const [first, second] = stackTags([label], [tag, tag]);
    expect(first).toBe(tag.bottom);
    expect(second).toBe(label.bottom - h - 2);
  });

  it("every portal label sits clear above its door and a standing player's name tag", () => {
    for (const m of maps) {
      const view = parseMap(
        m.id,
        JSON.parse(readFileSync(path.resolve(process.cwd(), `public/maps/${m.id}.tmj`), 'utf8')),
      );
      for (const p of view.portals) {
        const bottom = portalLabelY(p.area);
        expect(bottom, `${m.id} ${p.name}`).toBeLessThan(p.area.y);
        expect(bottom, `${m.id} ${p.name}`).toBeLessThan(p.area.y + p.area.h - NAME_TAG_TOP);
      }
    }
  });

  it.each([
    {
      name: 'a one-row solid is all surface',
      rect: { x: 0, y: 608, w: 1280, h: 32 },
      want: [{ texture: TEX.dirtMid, x: 0, y: 608, w: 1280, h: 32, tint: null }],
    },
    {
      name: 'a tall solid is a surface row and a fill below',
      rect: { x: 0, y: 0, w: 32, h: 608 },
      want: [
        { texture: TEX.dirtMid, x: 0, y: 0, w: 32, h: 32, tint: null },
        { texture: TEX.dirtCenter, x: 0, y: 32, w: 32, h: 576, tint: null },
      ],
    },
    {
      name: 'a solid thinner than a row is drawn at its own height',
      rect: { x: 10, y: 10, w: 64, h: 8 },
      want: [{ texture: TEX.dirtMid, x: 10, y: 10, w: 64, h: 8, tint: null }],
    },
  ])('$name', ({ rect, want }) => {
    expect(solidTiles([rect], dirt)).toEqual(want);
  });

  it('a thin platform is drawn as a full half tile hanging from its top edge', () => {
    expect(platformTiles([{ x: 160, y: 480, w: 256, h: 8 }], dirt)).toEqual([
      { texture: TEX.dirtHalf, x: 160, y: 480, w: 256, h: 32, tint: null },
    ]);
  });

  it('a rope is a ladder one tile wide centred on it with a top piece', () => {
    expect(ropeTiles([{ x: 1052, y: 448, w: 8, h: 160 }])).toEqual([
      { texture: TEX.ladderTop, x: 1040, y: 448, w: 32, h: 32, tint: null },
      { texture: TEX.ladderMid, x: 1040, y: 480, w: 32, h: 128, tint: null },
    ]);
  });

  it('a portal is a door top and a middle filling the rest', () => {
    expect(portalTiles(town.portals)).toEqual([
      { texture: TEX.doorClosedTop, x: 1200, y: 544, w: 32, h: 32, tint: null },
      { texture: TEX.doorClosedMid, x: 1200, y: 576, w: 32, h: 32, tint: null },
    ]);
  });

  it('the town draws its portal, rope, platforms and solids back to front', () => {
    const tiles = mapTiles(town);
    const textures = tiles.map((t) => t.texture);
    expect(textures.slice(0, 4)).toEqual([
      TEX.doorClosedTop,
      TEX.doorClosedMid,
      TEX.ladderTop,
      TEX.ladderMid,
    ]);
    expect(textures.filter((t) => t === TEX.dirtHalf)).toHaveLength(town.geometry.platforms.length);
    expect(textures.filter((t) => t === TEX.dirtMid)).toHaveLength(town.geometry.solids.length);
  });
});
