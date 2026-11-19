// The match's fighters as the world draws them, for their frames (pvp.md section 13): each one's level, HP and statuses as of the newest frame, and whether they're down. The world scene sets it; display only.
import type { ShownStatus } from './statuses';
import { defineStore } from './store';

export interface FighterLive {
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  readonly down: boolean;
  readonly statuses: readonly ShownStatus[];
}

export interface FightersState {
  /** By character id: the match's fighters but the player, once a frame has shown them. */
  readonly live: Readonly<Record<string, FighterLive>>;
  /** The fighters as drawn now; one the same as before is kept, so its frame doesn't render again. */
  show(next: Readonly<Record<string, FighterLive>>): void;
}

const same = (a: FighterLive, b: FighterLive) =>
  a.level === b.level &&
  a.hp === b.hp &&
  a.maxHp === b.maxHp &&
  a.down === b.down &&
  a.statuses.length === b.statuses.length &&
  a.statuses.every(
    (s, i) =>
      s.id === b.statuses[i]!.id &&
      s.from === b.statuses[i]!.from &&
      s.until === b.statuses[i]!.until,
  );

export const fighters = defineStore<FightersState>((set, get) => ({
  live: {},
  show: (next) => {
    const was = get().live;
    const live: Record<string, FighterLive> = {};
    let changed = Object.keys(next).length !== Object.keys(was).length;
    for (const [id, f] of Object.entries(next)) {
      const old = was[id];
      live[id] = old && same(old, f) ? old : f;
      changed ||= live[id] !== old;
    }
    if (changed) {
      set({ live });
    }
  },
}));
