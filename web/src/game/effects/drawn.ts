// Each effect's drawn version, by id: what plays wherever the manifest names no art.
import { hitsDrawn } from './hits';
import type { EffectId } from './ids';
import type { Drawn } from './types';

const drawn = new Map<EffectId, Drawn>([...(Object.entries(hitsDrawn) as [EffectId, Drawn][])]);

export function drawnVersion(id: EffectId): Drawn | undefined {
  return drawn.get(id);
}
