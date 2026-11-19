// The Combat tab's lines: this player's EXP, level-ups, promotions, loot, quests, potions and skill evolutions, kept until they leave the world.
import { skillById, stageAt } from '@/progression/skills';
import { LootKind, QuestChange } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';

import { defineStore } from './store';

export const MAX_COMBAT_LINES = 200;

export type CombatEvent =
  | { readonly kind: 'exp'; readonly amount: number }
  | { readonly kind: 'level'; readonly level: number }
  | { readonly kind: 'promoted'; readonly cls: string }
  | {
      readonly kind: 'loot';
      readonly loot: LootKind.TAKEN | LootKind.LEFT;
      readonly ownerId: string;
      readonly ownerName: string;
      readonly items: readonly LootLineItem[];
    }
  | {
      readonly kind: 'drank';
      readonly template: string;
      /** The HP it restored, as the client worked it out when it asked; the world draws over the number. */
      readonly hp: number;
    }
  | { readonly kind: 'evolved'; readonly from: string; readonly to: string }
  | {
      readonly kind: 'quest';
      readonly change: QuestLineKind;
      readonly quest: string;
      /** The log's counts after the change, for a progress line. */
      readonly progress: readonly number[];
    };

export type QuestLineKind = 'accepted' | 'progressed' | 'ready' | 'finished' | 'abandoned';

const questLines: Partial<Record<QuestChange, QuestLineKind>> = {
  [QuestChange.ACCEPTED]: 'accepted',
  [QuestChange.PROGRESSED]: 'progressed',
  [QuestChange.HANDED_IN]: 'finished',
  [QuestChange.ABANDONED]: 'abandoned',
};

export type CombatLine = CombatEvent & { readonly id: number };

export interface LootLineItem {
  readonly template: string;
  readonly quantity: number;
}

export interface CombatLogState {
  readonly lines: readonly CombatLine[];
  /** amount is negative on the death penalty. */
  applyExpGained(d: Data<'expGained'>): void;
  applyLevelUp(d: Data<'levelUp'>): void;
  applyPromoted(d: Data<'promoted'>): void;
  applyLoot(d: Data<'loot'>): void;
  applyQuestChanged(d: Data<'questChanged'>): void;
  /** The world applied a potion this player drank. */
  drank(template: string, hp: number): void;
  /** A line for each skill whose points before and after put it in a later stage. */
  applySkillPoints(
    before: Readonly<Record<string, number>>,
    after: Readonly<Record<string, number>>,
  ): void;
  /** A quest became ready to hand in: its last kill, item or level came. */
  questReady(quest: string): void;
  clear(): void;
}

let nextId = 1;

export const combatLog = defineStore<CombatLogState>((set, get) => {
  const add = (e: CombatEvent) => {
    const lines = [...get().lines, { ...e, id: nextId++ }];
    set({ lines: lines.slice(-MAX_COMBAT_LINES) });
  };
  return {
    lines: [],
    applyExpGained: (d) => {
      if (d.amount !== 0n) {
        add({ kind: 'exp', amount: Number(d.amount) });
      }
    },
    applyLevelUp: (d) => {
      if (d.character) {
        add({ kind: 'level', level: d.character.level });
      }
    },
    applyPromoted: (d) => {
      if (d.character) {
        add({ kind: 'promoted', cls: d.character.class });
      }
    },
    applyLoot: (d) => {
      const { kind, ownerId, ownerName } = d;
      if ((kind !== LootKind.TAKEN && kind !== LootKind.LEFT) || d.items.length === 0) {
        return; // no kind, or one from a newer server
      }
      const items = d.items.map(({ template, quantity }) => ({ template, quantity }));
      add({ kind: 'loot', loot: kind, ownerId, ownerName, items });
    },
    applyQuestChanged: (d) => {
      const change = questLines[d.change];
      if (change && d.quest) {
        add({ kind: 'quest', change, quest: d.quest.questId, progress: d.quest.progress });
      }
    },
    drank: (template, hp) => {
      add({ kind: 'drank', template, hp });
    },
    applySkillPoints: (before, after) => {
      for (const [id, points] of Object.entries(after)) {
        const skill = skillById(id);
        const from = skill?.stages[stageAt(skill, before[id] ?? 0)];
        const to = skill?.stages[stageAt(skill, points)];
        if (from && to && to.at > from.at) {
          add({ kind: 'evolved', from: from.name, to: to.name });
        }
      }
    },
    questReady: (quest) => {
      add({ kind: 'quest', change: 'ready', quest, progress: [] });
    },
    clear: () => {
      set({ lines: [] });
    },
  };
});
