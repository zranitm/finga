// The player's fight clock (skill-traits.md section 4): a Damage naming them, dealt or taken, keeps them in combat for the actions' combat time after it. The client hears it after the world's tick, so its clock ends after the world's and never offers a change the world would refuse.
import { actions } from '@/gamedata/actions';

import { defineStore } from './store';

export interface CombatState {
  readonly inCombat: boolean;
  /** A fight event named the player just now. */
  fought(): void;
  /** A join or a map change starts out of combat. */
  clear(): void;
}

let ends: ReturnType<typeof setTimeout> | undefined;

export const combat = defineStore<CombatState>((set, get) => ({
  inCombat: false,
  fought: () => {
    clearTimeout(ends);
    ends = setTimeout(() => set({ inCombat: false }), actions.combat_ms);
    if (!get().inCombat) {
      set({ inCombat: true });
    }
  },
  clear: () => {
    clearTimeout(ends);
    if (get().inCombat) {
      set({ inCombat: false });
    }
  },
}));
