import { useMemo } from 'react';

import type { Player } from '@/progression/quests';
import { character } from '@/state/character';
import { heldOf, inventory } from '@/state/inventory';
import { quests } from '@/state/quests';

/** The player as the quest rules read it, kept current with the log, the level and the inventory. */
export function useQuestPlayer(): Player {
  const log = quests.use((s) => s.log);
  const level = character.use((s) => s.character?.level ?? 1);
  const cls = character.use((s) => s.character?.class ?? '');
  const slots = inventory.use((s) => s.slots);
  return useMemo(
    () => ({ level, cls, log, held: (template: string) => heldOf(slots, template) }),
    [level, cls, log, slots],
  );
}
