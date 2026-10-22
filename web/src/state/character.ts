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
  /** The local player is dead until Respawned (C10). */
  readonly dead: boolean;
  /** The last ExpGained's amount (negative on the death penalty), with a counter so the HUD shows each one. */
  readonly lastExpChange: { readonly amount: number; readonly seq: number } | null;
  applyWelcome(d: Data<'Welcome'>): void;
  applyLevelUp(d: Data<'LevelUp'>): void;
  /** The server's new total within the level; amount may be negative (the death penalty). */
  applyExpGained(d: Data<'ExpGained'>): void;
  setVitals(hp: number, mp: number): void;
  setDead(dead: boolean): void;
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
  dead: false,
  lastExpChange: null,
  applyWelcome: (d) => {
    set({ character: d.character, expToNext: expToNextLevel(d.character.level), dead: false });
  },
  applyLevelUp: (d) => {
    set({ character: d.character, expToNext: expToNextLevel(d.character.level) });
  },
  applyExpGained: (d) => {
    const c = get().character;
    if (c) {
      const seq = (get().lastExpChange?.seq ?? 0) + 1;
      set({ character: { ...c, exp: d.exp }, lastExpChange: { amount: d.amount, seq } });
    }
  },
  setVitals: (hp, mp) => {
    const c = get().character;
    if (c && (c.hp !== hp || c.mp !== mp)) {
      set({ character: { ...c, hp, mp } });
    }
  },
  setDead: (dead) => {
    if (get().dead !== dead) {
      set({ dead });
    }
  },
  clear: () => {
    set({ character: null, expToNext: null, dead: false, lastExpChange: null });
  },
}));
