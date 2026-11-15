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
