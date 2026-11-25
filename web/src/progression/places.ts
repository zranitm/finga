// The places below a region and the gates between them (underground-and-underwater.md section 7), from game data alone: the map views mark a gate down, name the place with its levels, and show a party member on the other side at the gate.
import { maps } from '@/gamedata/maps';

type MapData = (typeof maps)[number];

/** The places below the region id: the regions whose region above it is. */
export function placesBelow(id: string): MapData[] {
  return maps.filter((m) => m.under === id);
}

/** The lowest and highest levels of a region's areas, a town's aside; null for one with none. */
export function levelsOf(map: {
  readonly areas: readonly { readonly levels: readonly [number, number] | null }[];
}): readonly [number, number] | null {
  const levels = map.areas.flatMap((a) => (a.levels ? [a.levels] : []));
  return levels.length > 0
    ? [Math.min(...levels.map((l) => l[0])), Math.max(...levels.map((l) => l[1]))]
    : null;
}

/** Where on map here the gate to there stands, when one is the place below the other: the cave mouth down, or the way back up; null for any other pair. */
export function gateBetween(here: string, there: string): { x: number; z: number } | null {
  const h = maps.find((m) => m.id === here);
  const t = maps.find((m) => m.id === there);
  if (!h || !t || (t.under !== here && h.under !== there)) {
    return null;
  }
  const gate = h.portals.find((p) => p.to === there);
  return gate ? { x: gate.x, z: gate.z } : null;
}
