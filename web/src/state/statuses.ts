// The statuses the HUD shows with their time left (statuses.md section 6): the player's own, the locked mob's, and those of party members on the player's map. The world scene sets them; display only.
import { defineStore } from './store';

/** A status as the HUD times it: from when it was first shown until it ends, in performance.now() milliseconds. */
export interface ShownStatus {
  readonly id: string;
  readonly from: number;
  readonly until: number;
}

export interface Shown {
  readonly me: readonly ShownStatus[];
  readonly target: readonly ShownStatus[];
  /** By character id: only members on the player's map, whose statuses the frames carry; null for one the frames don't bring, whose statuses nobody knows. */
  readonly party: Readonly<Record<string, readonly ShownStatus[] | null>>;
}

export interface StatusesState extends Shown {
  /** The statuses as drawn now; a list the same as before is kept, so its frame doesn't render again. */
  show(next: Shown): void;
}

const same = (a: readonly ShownStatus[], b: readonly ShownStatus[]) =>
  a.length === b.length &&
  a.every((s, i) => s.id === b[i]!.id && s.from === b[i]!.from && s.until === b[i]!.until);

const none: readonly ShownStatus[] = [];

export const statuses = defineStore<StatusesState>((set, get) => ({
  me: none,
  target: none,
  party: {},
  show: (next) => {
    const was = get();
    const keep = (a: readonly ShownStatus[], b: readonly ShownStatus[]) =>
      same(a, b) ? a : b.length === 0 ? none : b;
    const party: Record<string, readonly ShownStatus[] | null> = {};
    for (const [id, list] of Object.entries(next.party)) {
      party[id] = list && keep(was.party[id] ?? none, list);
    }
    const partySame =
      Object.keys(party).length === Object.keys(was.party).length &&
      Object.entries(party).every(([id, list]) => was.party[id] === list);
    const me = keep(was.me, next.me);
    const target = keep(was.target, next.target);
    if (me !== was.me || target !== was.target || !partySame) {
      set({ me, target, party: partySame ? was.party : party });
    }
  },
}));
