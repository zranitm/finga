// Per-tick positions never come through here; HP arrives only when it changes.
import { create } from '@bufbuild/protobuf';

import { expCurve } from '@/gamedata/exp-curve';
import { primaryKeys } from '@/progression/stats';
import { StatsSchema } from '@/protocol/messages';
import type { CharacterView, Data, StatSheet, Stats } from '@/protocol/messages';

import { defineStore } from './store';

export interface CharacterState {
  readonly character: CharacterView | null;
  /** null at the cap or with no character. */
  readonly expToNext: number | null;
  readonly dead: boolean;
  /** seq lets the HUD show two equal gains in a row. */
  readonly lastExpChange: { readonly amount: number; readonly seq: number } | null;
  applyWelcome(d: Data<'welcome'>): void;
  applyLevelUp(d: Data<'levelUp'>): void;
  /** The new class, with its sheet and HP and MP restored. */
  applyPromoted(d: Data<'promoted'>): void;
  /** HP or MP moved, with the maxima; when gear or points changed the sheet too, the new sheet and the own stats under it. Without a sheet, the last one stays. */
  applyStatsChanged(d: Data<'statsChanged'>): void;
  /** amount is negative on the death penalty. */
  applyExpGained(d: Data<'expGained'>): void;
  setVitals(hp: number, mp: number): void;
  setDead(dead: boolean): void;
  clear(): void;
}

export function expToNextLevel(level: number): number | null {
  if (level >= expCurve.level_cap) {
    return null;
  }
  const entry = expCurve.exp_to_next.find((e) => e.level === level);
  return entry ? entry.exp : null;
}

/** The character's own stats, as requirements read them: the sheet's totals less what the gear and the skills add. */
export function ownStats(sheet: StatSheet): Stats {
  return create(
    StatsSchema,
    Object.fromEntries(
      primaryKeys.map((k) => [
        k,
        (sheet.primary?.[k] ?? 0) - (sheet.primaryWorn?.[k] ?? 0) - (sheet.primarySkills?.[k] ?? 0),
      ]),
    ),
  );
}

export const character = defineStore<CharacterState>((set, get) => ({
  character: null,
  expToNext: null,
  dead: false,
  lastExpChange: null,
  applyWelcome: (d) => {
    const c = d.character ?? null;
    set({ character: c, expToNext: c ? expToNextLevel(c.level) : null, dead: false });
  },
  applyLevelUp: (d) => {
    if (d.character) {
      set({ character: d.character, expToNext: expToNextLevel(d.character.level) });
    }
  },
  applyPromoted: (d) => {
    if (d.character) {
      set({ character: d.character });
    }
  },
  applyStatsChanged: (d) => {
    const c = get().character;
    if (c) {
      set({
        character: {
          ...c,
          sheet: d.sheet ?? c.sheet,
          stats: d.sheet ? ownStats(d.sheet) : c.stats,
          hp: d.hp,
          maxHp: d.maxHp,
          mp: d.mp,
          maxMp: d.maxMp,
        },
      });
    }
  },
  applyExpGained: (d) => {
    const c = get().character;
    if (c) {
      const seq = (get().lastExpChange?.seq ?? 0) + 1;
      set({ character: { ...c, exp: d.exp }, lastExpChange: { amount: Number(d.amount), seq } });
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
