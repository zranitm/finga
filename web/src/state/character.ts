// Per-tick positions never come through here; HP arrives only when it changes.
import { create } from '@bufbuild/protobuf';

import { expCurve } from '@/gamedata/exp-curve';
import { skillById, stageAt } from '@/progression/skills';
import { primaryKeys } from '@/progression/stats';
import { StatsSchema } from '@/protocol/messages';
import type { CharacterView, Data, StatSheet, Stats } from '@/protocol/messages';

import { defineStore } from './store';

/** A buff on the character as the latest sheet lists it (frame-icons.md section 4), timed in performance.now() milliseconds: counted down from `from` to `until`. */
export interface FrameBuff {
  readonly skill: string;
  /** The points it was used with, which pick its stage. */
  readonly points: number;
  /** The character who used it: '' for the character's own, or one whose giver a join forgot. */
  readonly giver: string;
  readonly from: number;
  readonly until: number;
}

export interface CharacterState {
  readonly character: CharacterView | null;
  /** The buffs running on the character, from the latest sheet. */
  readonly buffs: readonly FrameBuff[];
  /** null at the cap or with no character. */
  readonly expToNext: number | null;
  readonly dead: boolean;
  /** What's left of the character's shields, which take mobs' damage before HP. */
  readonly shield: number;
  /** seq lets the HUD show two equal gains in a row. */
  readonly lastExpChange: { readonly amount: number; readonly seq: number } | null;
  applyWelcome(d: Data<'welcome'>): void;
  applyLevelUp(d: Data<'levelUp'>): void;
  /** The new class, with its sheet and HP restored. */
  applyPromoted(d: Data<'promoted'>): void;
  /** HP moved, with its maximum; when gear or points changed the sheet too, the new sheet and the own stats under it. Without a sheet, the last one stays. */
  applyStatsChanged(d: Data<'statsChanged'>): void;
  /** amount is negative on the death penalty. */
  applyExpGained(d: Data<'expGained'>): void;
  setHp(hp: number): void;
  setShield(shield: number): void;
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

/** Two sheets' ends of one buff closer than this are the same use, apart by the network: a buff used again ends at least its cooldown later. */
const SAME_USE_MS = 300;

const noBuffs: readonly FrameBuff[] = [];

/** A sheet's buffs, each ending its left_ms after now. One the list had, from the same giver and use, keeps its times, so a sheet sent for anything else leaves its sweep alone; a new one sweeps from its whole duration, as one joined with part-way shows part-way. */
export function buffsOf(
  sheet: StatSheet | undefined,
  was: readonly FrameBuff[],
  now: number,
): readonly FrameBuff[] {
  const next = (sheet?.buffs ?? []).map((b): FrameBuff => {
    const left = Number(b.leftMs);
    const until = now + left;
    const kept = was.find(
      (w) =>
        w.skill === b.skill &&
        w.giver === b.from &&
        w.points === b.points &&
        Math.abs(w.until - until) <= SAME_USE_MS,
    );
    if (kept) {
      return kept;
    }
    const s = skillById(b.skill);
    const whole = s ? (s.stages[stageAt(s, b.points)]?.duration_ms ?? 0) : 0;
    return {
      skill: b.skill,
      points: b.points,
      giver: b.from,
      from: until - Math.max(whole, left),
      until,
    };
  });
  if (next.length === 0) {
    return noBuffs;
  }
  return next.length === was.length && next.every((b, k) => b === was[k]) ? was : next;
}

export const character = defineStore<CharacterState>((set, get) => ({
  character: null,
  buffs: noBuffs,
  expToNext: null,
  dead: false,
  shield: 0,
  lastExpChange: null,
  applyWelcome: (d) => {
    const c = d.character ?? null;
    set({
      character: c,
      buffs: buffsOf(c?.sheet, noBuffs, performance.now()),
      expToNext: c ? expToNextLevel(c.level) : null,
      dead: false,
      shield: 0,
    });
  },
  applyLevelUp: (d) => {
    if (d.character) {
      set({
        character: d.character,
        buffs: buffsOf(d.character.sheet, get().buffs, performance.now()),
        expToNext: expToNextLevel(d.character.level),
      });
    }
  },
  applyPromoted: (d) => {
    if (d.character) {
      set({
        character: d.character,
        buffs: buffsOf(d.character.sheet, get().buffs, performance.now()),
      });
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
        },
        buffs: d.sheet ? buffsOf(d.sheet, get().buffs, performance.now()) : get().buffs,
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
  setHp: (hp) => {
    const c = get().character;
    if (c && c.hp !== hp) {
      set({ character: { ...c, hp } });
    }
  },
  setShield: (shield) => {
    if (get().shield !== shield) {
      set({ shield });
    }
  },
  setDead: (dead) => {
    if (get().dead !== dead) {
      set({ dead });
    }
  },
  clear: () => {
    set({
      character: null,
      buffs: noBuffs,
      expToNext: null,
      dead: false,
      shield: 0,
      lastExpChange: null,
    });
  },
}));
