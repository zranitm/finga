// The waypoints the character has found, by id across every region (large-regions.md section 8): the Welcome's list, then each WaypointFound.
import { defineStore } from './store';

export interface WaypointsState {
  readonly found: ReadonlySet<string>;
  readonly last: string | null; // the latest a WaypointFound told of, whose stone flares; a join's list flares none
  /** A join: the character's found waypoints, in place of any before. */
  applyWelcome(ids: readonly string[]): void;
  find(id: string): void;
  clear(): void;
}

export const waypoints = defineStore<WaypointsState>((set, get) => ({
  found: new Set(),
  last: null,
  applyWelcome: (ids) => {
    set({ found: new Set(ids) });
  },
  find: (id) => {
    if (!get().found.has(id)) {
      set({ found: new Set([...get().found, id]), last: id });
    }
  },
  clear: () => {
    set({ found: new Set(), last: null });
  },
}));
