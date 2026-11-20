// Choosing a trait for the arena build: every trait is open and nothing waits on the world, so a choice lands at once and is saved with the build.
import type { Skill } from '@/progression/skills';
import { arenaBuild } from '@/state/arena-build';

import type { TraitChoice } from './useTraits';

export function useArenaTraits(): TraitChoice {
  const traits = arenaBuild.use((s) => s.traits);
  const fallback = (skill: Skill) => skill.traits[0]?.id ?? '';
  return {
    chosen: (skill) => traits[skill.id] ?? fallback(skill),
    pending: null,
    notice: null,
    // The default is kept as none, as the world keeps it.
    choose: (skill, trait) => {
      arenaBuild.getState().choose(skill.id, trait === fallback(skill) ? '' : trait);
    },
  };
}
