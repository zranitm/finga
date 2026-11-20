// The names game data gives mobs and maps, for lines the client writes itself.
import { maps } from '@/gamedata/maps';

import { mobById } from './mobs';

/** The mob type's name, or its ID for one this client doesn't know. */
export function mobName(type: string): string {
  return mobById(type)?.name ?? type;
}

/** The map's name, or its ID for one this client doesn't know. */
export function mapName(id: string): string {
  return maps.find((m) => m.id === id)?.name ?? id;
}

/** Whether the map is a match's (pvp.md section 3). */
export function isMatchMap(id: string): boolean {
  return maps.find((m) => m.id === id)?.kind === 'pvp';
}

/** Where a party member is, as their frame says it: a match's map is "In a match" (pvp.md section 4), a region's names its channel. */
export function whereIs(mapId: string, channel: number): string {
  return isMatchMap(mapId) ? 'In a match' : withChannel(mapName(mapId), channel);
}

/** name with " · Ch. n" after it for channel n of a region; channel 0, off regions, adds nothing (large-regions.md section 4). */
export function withChannel(name: string, channel: number): string {
  return channel > 0 ? `${name} · Ch. ${channel}` : name;
}
