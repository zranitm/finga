// Each mob type's game data, by its ID.
import { mobs } from '@/gamedata/mobs';

export type MobType = (typeof mobs)[number];

const byId = new Map<string, MobType>(mobs.map((m) => [m.id, m]));

/** The mob type with id, if this client knows it. */
export function mobById(id: string | undefined): MobType | undefined {
  return id === undefined ? undefined : byId.get(id);
}
