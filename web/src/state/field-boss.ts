// The map's field boss: up, for the boss bar, whose HP the world scene draws; or beaten, for the map's mark counting down to when it's back.
import type { Data } from '@/protocol/messages';

import { defineStore } from './store';

export interface FieldBoss {
  readonly mobId: number;
  readonly type: string;
}

export interface BossDown {
  readonly type: string;
  readonly backAt: number; // performance.now() milliseconds
}

export interface BossHp {
  readonly hp: number;
  readonly maxHp: number;
}

export interface FieldBossState {
  readonly boss: FieldBoss | null;
  readonly down: BossDown | null;
  /** While the world scene draws the boss. */
  readonly hp: BossHp | null;
  /** A Snapshot's: the boss up, beaten, or neither on a map without one. */
  applySnapshot(up: Data<'fieldBoss'> | undefined, down: Data<'fieldBossGone'> | undefined): void;
  applyFieldBoss(d: Data<'fieldBoss'>): void;
  applyGone(d: Data<'fieldBossGone'>): void;
  setHp(hp: BossHp | null): void;
  clear(): void;
}

const downOf = (d: Data<'fieldBossGone'>): BossDown => ({
  type: d.type,
  backAt: performance.now() + Number(d.backInMs),
});

export const fieldBoss = defineStore<FieldBossState>((set, get) => ({
  boss: null,
  down: null,
  hp: null,
  applySnapshot: (up, down) => {
    const same = up !== undefined && get().boss?.mobId === up.mobId; // a rejoin's, the scene's HP still good
    set({
      boss: up ? { mobId: up.mobId, type: up.type } : null,
      down: down ? downOf(down) : null,
      ...(!same && { hp: null }),
    });
  },
  applyFieldBoss: (d) => {
    const same = get().boss?.mobId === d.mobId;
    set({ boss: { mobId: d.mobId, type: d.type }, down: null, ...(!same && { hp: null }) });
  },
  applyGone: (d) => {
    set({ boss: null, down: downOf(d), hp: null });
  },
  setHp: (hp) => {
    const now = get().hp;
    if (now?.hp !== hp?.hp || now?.maxHp !== hp?.maxHp) {
      set({ hp });
    }
  },
  clear: () => {
    set({ boss: null, down: null, hp: null });
  },
}));
