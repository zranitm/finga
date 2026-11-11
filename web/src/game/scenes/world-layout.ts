import { maps } from '@/gamedata/maps';
import type { MapView } from '@/sim/map';
import type { Rect } from '@/sim/step';

import { TEX, TILE } from '../keys';
import type { TextureKey } from '../keys';

export interface GroundStyle {
  readonly top: TextureKey;
  readonly fill: TextureKey;
  readonly platform: TextureKey;
  readonly background: TextureKey;
}

interface Ground {
  readonly top: TextureKey;
  readonly fill: TextureKey;
  readonly platform: TextureKey;
}

type GroundName = (typeof maps)[number]['ground'];
type BackgroundName = (typeof maps)[number]['background'];

const grounds = {
  dirt: { top: TEX.dirtMid, fill: TEX.dirtCenter, platform: TEX.dirtHalf },
  grass: { top: TEX.grassMid, fill: TEX.grassCenter, platform: TEX.grassHalf },
  stone: { top: TEX.stoneMid, fill: TEX.stoneCenter, platform: TEX.stoneHalf },
};

const backgrounds = {
  land: TEX.bgLand,
  grass: TEX.bgGrass,
  cave: TEX.bgCave,
};

// A ground or background a map names but these tables lack fails the type check.
const _grounds: Record<GroundName, Ground> = grounds;
const _backgrounds: Record<BackgroundName, TextureKey> = backgrounds;
void _grounds;
void _backgrounds;

export function groundStyle(mapId: string): GroundStyle {
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    throw new Error(`world-layout: map ${mapId} is not in the game data`);
  }
  return { ...grounds[map.ground], background: backgrounds[map.background] };
}

/** The banner shown on entering a map: its name, and for a hunting map its level range. */
export function bannerLines(mapId: string): string[] {
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    throw new Error(`world-layout: map ${mapId} is not in the game data`);
  }
  return map.kind === 'hunting' ? [map.name, `Lv ${map.min_level}–${map.max_level}`] : [map.name];
}

export interface Tile {
  readonly texture: TextureKey;
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
}

export function solidTiles(solids: readonly Rect[], s: GroundStyle): Tile[] {
  const out: Tile[] = [];
  for (const r of solids) {
    out.push({ texture: s.top, x: r.x, y: r.y, w: r.w, h: Math.min(TILE, r.h) });
    if (r.h > TILE) {
      out.push({ texture: s.fill, x: r.x, y: r.y + TILE, w: r.w, h: r.h - TILE });
    }
  }
  return out;
}

/** Half-tile art hangs from the top edge, whatever the platform's height. */
export function platformTiles(platforms: readonly Rect[], s: GroundStyle): Tile[] {
  return platforms.map((r) => ({ texture: s.platform, x: r.x, y: r.y, w: r.w, h: TILE }));
}

export function ropeTiles(ropes: readonly Rect[]): Tile[] {
  const out: Tile[] = [];
  for (const r of ropes) {
    const x = r.x + r.w / 2 - TILE / 2;
    out.push({ texture: TEX.ladderTop, x, y: r.y, w: TILE, h: Math.min(TILE, r.h) });
    if (r.h > TILE) {
      out.push({ texture: TEX.ladderMid, x, y: r.y + TILE, w: TILE, h: r.h - TILE });
    }
  }
  return out;
}

export function portalTiles(portals: MapView['portals']): Tile[] {
  const out: Tile[] = [];
  for (const p of portals) {
    const a = p.area;
    out.push({ texture: TEX.doorClosedTop, x: a.x, y: a.y, w: a.w, h: Math.min(TILE, a.h) });
    if (a.h > TILE) {
      out.push({ texture: TEX.doorClosedMid, x: a.x, y: a.y + TILE, w: a.w, h: a.h - TILE });
    }
  }
  return out;
}

/** Back to front. */
export function mapTiles(view: MapView): Tile[] {
  const s = groundStyle(view.id);
  return [
    ...portalTiles(view.portals),
    ...ropeTiles(view.geometry.ropes),
    ...platformTiles(view.geometry.platforms, s),
    ...solidTiles(view.geometry.solids, s),
  ];
}
