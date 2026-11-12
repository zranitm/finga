// The map's field boss while it is up, for the boss bar: its time left counts from when the message came, and its HP is what the world scene draws.
import type { Data } from '@/protocol/messages';

import { defineStore } from './store';

export interface FieldBoss {
  readonly mobId: number;
  readonly type: string;
  readonly leavesAt: number; // performance.now() milliseconds
}

export interface BossHp {
  readonly hp: number;
  readonly maxHp: number;
}

export interface FieldBossState {
  readonly boss: FieldBoss | null;
  /** While the world scene draws the boss. */
  readonly hp: BossHp | null;
  /** A FieldBoss, or a Snapshot's, which has none when no boss is up. */
  applyFieldBoss(d: Data<'fieldBoss'> | undefined): void;
  /** The boss that went, if it was this map's: the leaving notice names it. */
  applyGone(d: Data<'fieldBossGone'>): FieldBoss | null;
  setHp(hp: BossHp | null): void;
  clear(): void;
}

export const fieldBoss = defineStore<FieldBossState>((set, get) => ({
  boss: null,
  hp: null,
  applyFieldBoss: (d) => {
    if (!d) {
      set({ boss: null, hp: null });
      return;
    }
    const same = get().boss?.mobId === d.mobId;
    set({
      boss: { mobId: d.mobId, type: d.type, leavesAt: performance.now() + Number(d.leavesInMs) },
      ...(!same && { hp: null }),
    });
  },
  applyGone: (d) => {
    const boss = get().boss;
    if (boss?.mobId !== d.mobId) {
      return null;
    }
    set({ boss: null, hp: null });
    return boss;
  },
  setHp: (hp) => {
    const now = get().hp;
    if (now?.hp !== hp?.hp || now?.maxHp !== hp?.maxHp) {
      set({ hp });
    }
  },
  clear: () => {
    set({ boss: null, hp: null });
  },
}));
