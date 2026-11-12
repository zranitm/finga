// The Combat tab's lines: this player's EXP, level-ups, promotions, loot, quests, potions and skill evolutions, kept until they leave the world.
import { loot } from '@/gamedata/loot';
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
      readonly loot: LootKind.COLLECTED | LootKind.WAITING | LootKind.LOST;
      readonly ownerId: string;
      readonly ownerName: string;
      readonly template: string;
      readonly quantity: number;
    }
  | {
      readonly kind: 'drank';
      readonly template: string;
      /** What it restored, as the client worked it out when it asked; the world draws over each number. */
      readonly hp: number;
      readonly mp: number;
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

/** A drop of this player's waiting for room, as map/drop since drop IDs are a map's own. */
export interface Waiting {
  readonly key: string;
  /** No drop waits past its life, so one never heard of again is forgotten then (Unix ms). */
  readonly until: number;
}

export interface CombatLogState {
  readonly lines: readonly CombatLine[];
  readonly waiting: readonly Waiting[];
  /** amount is negative on the death penalty. */
  applyExpGained(d: Data<'expGained'>): void;
  applyLevelUp(d: Data<'levelUp'>): void;
  applyPromoted(d: Data<'promoted'>): void;
  applyLoot(d: Data<'loot'>): void;
  applyQuestChanged(d: Data<'questChanged'>): void;
  /** The world applied a potion this player drank. */
  drank(template: string, restored: { readonly hp: number; readonly mp: number }): void;
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
  const expire = () => {
    const { waiting } = get();
    const now = Date.now();
    const live = waiting.filter((w) => w.until > now);
    if (live.length !== waiting.length) {
      set({ waiting: live });
    }
  };
  return {
    lines: [],
    waiting: [],
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
      const { kind, ownerId, ownerName, template, quantity } = d;
      if (kind !== LootKind.COLLECTED && kind !== LootKind.WAITING && kind !== LootKind.LOST) {
        return; // no kind, or one from a newer server
      }
      add({ kind: 'loot', loot: kind, ownerId, ownerName, template, quantity });
      const key = `${d.mapId}/${d.dropId}`;
      const { waiting } = get();
      const others = waiting.filter((w) => w.key !== key);
      if (kind === LootKind.WAITING) {
        set({ waiting: [...others, { key, until: Date.now() + loot.drop_life_ms }] });
        setTimeout(expire, loot.drop_life_ms);
      } else if (others.length !== waiting.length) {
        set({ waiting: others });
      }
    },
    applyQuestChanged: (d) => {
      const change = questLines[d.change];
      if (change && d.quest) {
        add({ kind: 'quest', change, quest: d.quest.questId, progress: d.quest.progress });
      }
    },
    drank: (template, { hp, mp }) => {
      add({ kind: 'drank', template, hp, mp });
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
      set({ lines: [], waiting: [] });
    },
  };
});
