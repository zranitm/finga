// The names game data gives mobs and maps, for lines the client writes itself.
import { maps } from '@/gamedata/maps';
import { mobs } from '@/gamedata/mobs';

/** The mob type's name, or its ID for one this client doesn't know. */
export function mobName(type: string): string {
  return mobs.find((m) => m.id === type)?.name ?? type;
}

/** The map's name, or its ID for one this client doesn't know. */
export function mapName(id: string): string {
  return maps.find((m) => m.id === id)?.name ?? id;
}
