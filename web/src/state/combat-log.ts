// The Combat tab's lines: this player's EXP, level-ups and loot, kept until they leave the world.
import { loot } from '@/gamedata/loot';
import { LootKind } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';

import { defineStore } from './store';

export const MAX_COMBAT_LINES = 200;

export type CombatEvent =
  | { readonly kind: 'exp'; readonly amount: number }
  | { readonly kind: 'level'; readonly level: number }
  | {
      readonly kind: 'loot';
      readonly loot: LootKind.COLLECTED | LootKind.WAITING | LootKind.LOST;
      readonly ownerId: string;
      readonly ownerName: string;
      readonly template: string;
      readonly quantity: number;
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
  applyLoot(d: Data<'loot'>): void;
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
    clear: () => {
      set({ lines: [], waiting: [] });
    },
  };
});
