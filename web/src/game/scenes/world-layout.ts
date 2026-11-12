import { maps } from '@/gamedata/maps';
import type { MapView } from '@/sim/map';
import type { Rect } from '@/sim/step';

import { NAME_TAG_TOP } from '../entities/name-tag';
import { TEX, TILE } from '../keys';
import type { TextureKey } from '../keys';

export interface GroundStyle extends Ground {
  readonly background: TextureKey;
  readonly backgroundTint: number | null;
}

// A tint shades other art for what the packs in public/assets have none of, such as the keep's castle or a snowy sky.
interface Ground {
  readonly top: TextureKey;
  readonly fill: TextureKey;
  readonly platform: TextureKey;
  readonly tint: number | null;
}

interface Background {
  readonly texture: TextureKey;
  readonly tint: number | null;
}

type GroundName = (typeof maps)[number]['ground'];
type BackgroundName = (typeof maps)[number]['background'];

const grounds = {
  dirt: { top: TEX.dirtMid, fill: TEX.dirtCenter, platform: TEX.dirtHalf, tint: null },
  grass: { top: TEX.grassMid, fill: TEX.grassCenter, platform: TEX.grassHalf, tint: null },
  stone: { top: TEX.stoneMid, fill: TEX.stoneCenter, platform: TEX.stoneHalf, tint: null },
  sand: { top: TEX.sandMid, fill: TEX.sandCenter, platform: TEX.sandHalf, tint: null },
  snow: { top: TEX.snowMid, fill: TEX.snowCenter, platform: TEX.snowHalf, tint: null },
  brick: { top: TEX.stoneMid, fill: TEX.stoneCenter, platform: TEX.stoneHalf, tint: 0x8c7f9e },
};

const backgrounds = {
  land: { texture: TEX.bgLand, tint: null },
  grass: { texture: TEX.bgGrass, tint: null },
  cave: { texture: TEX.bgCave, tint: null },
  shroom: { texture: TEX.bgShroom, tint: null },
  dawn: { texture: TEX.bgLand, tint: 0xffd8b0 },
  desert: { texture: TEX.bgDesert, tint: null },
  snow: { texture: TEX.bgCave, tint: 0xc8dcf2 },
  dusk: { texture: TEX.bgLand, tint: 0x8a7aa8 },
};

// A ground or background a map names but these tables lack fails the type check.
const _grounds: Record<GroundName, Ground> = grounds;
const _backgrounds: Record<BackgroundName, Background> = backgrounds;
void _grounds;
void _backgrounds;

export function groundStyle(mapId: string): GroundStyle {
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    throw new Error(`world-layout: map ${mapId} is not in the game data`);
  }
  const bg = backgrounds[map.background];
  return { ...grounds[map.ground], background: bg.texture, backgroundTint: bg.tint };
}

/** The banner shown on entering a map: its name, and for a hunting map its level range. */
export function bannerLines(mapId: string): string[] {
  const map = maps.find((m) => m.id === mapId);
  if (!map) {
    throw new Error(`world-layout: map ${mapId} is not in the game data`);
  }
  return map.kind === 'hunting' ? [map.name, `Lv ${map.min_level}–${map.max_level}`] : [map.name];
}

/** The label over a portal: where it leads, as that map's banner says it, on one line. */
export function portalLabel(targetMap: string): string {
  return bannerLines(targetMap).join(' · ');
}

const PORTAL_LABEL_GAP = 6;

/** The bottom of a portal's label: above the door, and above the name tag of a player standing in it. */
export function portalLabelY(area: Rect): number {
  return Math.min(area.y, area.y + area.h - NAME_TAG_TOP) - PORTAL_LABEL_GAP;
}

export interface Tile {
  readonly texture: TextureKey;
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
  readonly tint: number | null;
}

export function solidTiles(solids: readonly Rect[], s: GroundStyle): Tile[] {
  const out: Tile[] = [];
  for (const r of solids) {
    out.push({ texture: s.top, x: r.x, y: r.y, w: r.w, h: Math.min(TILE, r.h), tint: s.tint });
    if (r.h > TILE) {
      out.push({ texture: s.fill, x: r.x, y: r.y + TILE, w: r.w, h: r.h - TILE, tint: s.tint });
    }
  }
  return out;
}

/** Half-tile art hangs from the top edge, whatever the platform's height. */
export function platformTiles(platforms: readonly Rect[], s: GroundStyle): Tile[] {
  return platforms.map((r) => ({
    texture: s.platform,
    x: r.x,
    y: r.y,
    w: r.w,
    h: TILE,
    tint: s.tint,
  }));
}

export function ropeTiles(ropes: readonly Rect[]): Tile[] {
  const out: Tile[] = [];
  for (const r of ropes) {
    const x = r.x + r.w / 2 - TILE / 2;
    out.push({ texture: TEX.ladderTop, x, y: r.y, w: TILE, h: Math.min(TILE, r.h), tint: null });
    if (r.h > TILE) {
      out.push({ texture: TEX.ladderMid, x, y: r.y + TILE, w: TILE, h: r.h - TILE, tint: null });
    }
  }
  return out;
}

export function portalTiles(portals: MapView['portals']): Tile[] {
  const out: Tile[] = [];
  for (const p of portals) {
    const a = p.area;
    out.push({
      texture: TEX.doorClosedTop,
      x: a.x,
      y: a.y,
      w: a.w,
      h: Math.min(TILE, a.h),
      tint: null,
    });
    if (a.h > TILE) {
      out.push({
        texture: TEX.doorClosedMid,
        x: a.x,
        y: a.y + TILE,
        w: a.w,
        h: a.h - TILE,
        tint: null,
      });
    }
  }
  return out;
}

/** Back to front. */
/** The ground's fill under the map, out to the window's right and bottom: a window taller than the map shows below its floor, since the camera holds a smaller map at the top left, and the background would show there. Null when the map fills the window's height. */
export function belowFloor(
  view: MapView,
  viewport: { readonly width: number; readonly height: number },
): Tile | null {
  const h = viewport.height - view.height;
  if (h <= 0) {
    return null;
  }
  const s = groundStyle(view.id);
  return {
    texture: s.fill,
    x: 0,
    y: view.height,
    w: Math.max(view.width, viewport.width),
    h,
    tint: s.tint,
  };
}

export function mapTiles(view: MapView): Tile[] {
  const s = groundStyle(view.id);
  return [
    ...portalTiles(view.portals),
    ...ropeTiles(view.geometry.ropes),
    ...platformTiles(view.geometry.platforms, s),
    ...solidTiles(view.geometry.solids, s),
  ];
}
