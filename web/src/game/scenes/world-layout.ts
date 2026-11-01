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

const forest: GroundStyle = {
  top: TEX.grassMid,
  fill: TEX.grassCenter,
  platform: TEX.grassHalf,
  background: TEX.bgGrass,
};

const styles: Readonly<Record<string, GroundStyle>> = {
  town: { top: TEX.dirtMid, fill: TEX.dirtCenter, platform: TEX.dirtHalf, background: TEX.bgLand },
  forest,
  cave: {
    top: TEX.stoneMid,
    fill: TEX.stoneCenter,
    platform: TEX.stoneHalf,
    background: TEX.bgCave,
  },
};

export function groundStyle(mapId: string): GroundStyle {
  return styles[mapId] ?? forest;
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
