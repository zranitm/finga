// Pure helpers for the World scene: what to draw for a map's object
// layers, as plain data the scene turns into tile sprites. Tested without
// Phaser.
import type { MapView } from '@/sim/map';
import type { Rect } from '@/sim/step';

import { TEX, TILE } from '../keys';
import type { TextureKey } from '../keys';

export interface GroundStyle {
  readonly top: TextureKey; // the surface row of a solid
  readonly fill: TextureKey; // rows below the surface
  readonly platform: TextureKey; // one-way platforms
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

/** The look of a map; an unknown map id gets the forest look rather than nothing. */
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

/** Every solid as a surface row plus a fill below it; a solid thinner than a row is all surface. */
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

/** A platform is drawn as a half tile hanging from its top edge, whatever its (thin) height. */
export function platformTiles(platforms: readonly Rect[], s: GroundStyle): Tile[] {
  return platforms.map((r) => ({ texture: s.platform, x: r.x, y: r.y, w: r.w, h: TILE }));
}

/** A rope is a ladder one tile wide centred on it, with a top piece. */
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

/** A portal is a door: a top piece and a middle that fills the rest of its area. */
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

/** All the tiles of a map, back to front. */
export function mapTiles(view: MapView): Tile[] {
  const s = groundStyle(view.id);
  return [
    ...portalTiles(view.portals),
    ...ropeTiles(view.geometry.ropes),
    ...platformTiles(view.geometry.platforms, s),
    ...solidTiles(view.geometry.solids, s),
  ];
}
