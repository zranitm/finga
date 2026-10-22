import { readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { parseMap } from '@/sim/map';

import { TEX } from '../keys';

import {
  groundStyle,
  mapTiles,
  platformTiles,
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
    { map: 'somewhere_new', top: TEX.grassMid, background: TEX.bgGrass },
  ])('$map looks like $top', ({ map, top, background }) => {
    expect(groundStyle(map)).toMatchObject({ top, background });
  });

  it.each([
    {
      name: 'a one-row solid is all surface',
      rect: { x: 0, y: 608, w: 1280, h: 32 },
      want: [{ texture: TEX.dirtMid, x: 0, y: 608, w: 1280, h: 32 }],
    },
    {
      name: 'a tall solid is a surface row and a fill below',
      rect: { x: 0, y: 0, w: 32, h: 608 },
      want: [
        { texture: TEX.dirtMid, x: 0, y: 0, w: 32, h: 32 },
        { texture: TEX.dirtCenter, x: 0, y: 32, w: 32, h: 576 },
      ],
    },
    {
      name: 'a solid thinner than a row is drawn at its own height',
      rect: { x: 10, y: 10, w: 64, h: 8 },
      want: [{ texture: TEX.dirtMid, x: 10, y: 10, w: 64, h: 8 }],
    },
  ])('$name', ({ rect, want }) => {
    expect(solidTiles([rect], dirt)).toEqual(want);
  });

  it('a thin platform is drawn as a full half tile hanging from its top edge', () => {
    expect(platformTiles([{ x: 160, y: 480, w: 256, h: 8 }], dirt)).toEqual([
      { texture: TEX.dirtHalf, x: 160, y: 480, w: 256, h: 32 },
    ]);
  });

  it('a rope is a ladder one tile wide centred on it with a top piece', () => {
    expect(ropeTiles([{ x: 1052, y: 448, w: 8, h: 160 }])).toEqual([
      { texture: TEX.ladderTop, x: 1040, y: 448, w: 32, h: 32 },
      { texture: TEX.ladderMid, x: 1040, y: 480, w: 32, h: 128 },
    ]);
  });

  it('a portal is a door top and a middle filling the rest', () => {
    expect(portalTiles(town.portals)).toEqual([
      { texture: TEX.doorClosedTop, x: 1200, y: 544, w: 32, h: 32 },
      { texture: TEX.doorClosedMid, x: 1200, y: 576, w: 32, h: 32 },
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
