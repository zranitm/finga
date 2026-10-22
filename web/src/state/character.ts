// The Welcome character: level, EXP, stats, HP and MP with their maxima,
// and the EXP the next level needs from the generated curve. Own HP and MP
// arrive from game/systems when they change (page 7); per-tick positions
// never come through here.
import { expCurve } from '@/gamedata/exp-curve';
import type { CharacterView, Data } from '@/protocol/messages';

import { defineStore } from './store';

export interface CharacterState {
  readonly character: CharacterView | null;
  /** EXP needed to reach the next level; null at the cap or with no character. */
  readonly expToNext: number | null;
  applyWelcome(d: Data<'Welcome'>): void;
  applyLevelUp(d: Data<'LevelUp'>): void;
  /** The server's new total within the level; amount may be negative (the death penalty). */
  applyExpGained(d: Data<'ExpGained'>): void;
  setVitals(hp: number, mp: number): void;
  clear(): void;
}

/** EXP needed to go from level to the next, from data/exp_curve.yaml; null at the cap. */
export function expToNextLevel(level: number): number | null {
  if (level >= expCurve.level_cap) {
    return null;
  }
  const entry = expCurve.exp_to_next.find((e) => e.level === level);
  return entry ? entry.exp : null;
}

export const character = defineStore<CharacterState>((set, get) => ({
  character: null,
  expToNext: null,
  applyWelcome: (d) => {
    set({ character: d.character, expToNext: expToNextLevel(d.character.level) });
  },
  applyLevelUp: (d) => {
    set({ character: d.character, expToNext: expToNextLevel(d.character.level) });
  },
  applyExpGained: (d) => {
    const c = get().character;
    if (c) {
      set({ character: { ...c, exp: d.exp } });
    }
  },
  setVitals: (hp, mp) => {
    const c = get().character;
    if (c && (c.hp !== hp || c.mp !== mp)) {
      set({ character: { ...c, hp, mp } });
    }
  },
  clear: () => {
    set({ character: null, expToNext: null });
  },
}));
