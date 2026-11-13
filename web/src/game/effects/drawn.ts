// Each effect's drawn version, by id: what plays wherever the manifest names no art.
import { skillById } from '@/progression/skills';
import type { Skill } from '@/progression/skills';

import { afterimage } from './afterimage';
import { hitsDrawn } from './hits';
import { effectIds } from './ids';
import type { EffectId } from './ids';
import { looks } from './looks';
import { mobLooks } from './mob-looks';
import type { Drawn } from './types';

const active = effectIds
  .map((id) => (id.startsWith('skill.') ? skillById(id.slice('skill.'.length)) : undefined))
  .filter((s): s is Skill => s !== undefined);

const drawn = new Map<EffectId, Drawn>([
  ...(Object.entries(hitsDrawn) as [EffectId, Drawn][]),
  ['cancel.afterimage', afterimage],
  ...looks(active),
  ...mobLooks(),
]);

export function drawnVersion(id: EffectId): Drawn | undefined {
  return drawn.get(id);
}
