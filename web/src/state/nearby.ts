// Set only when someone arrives or leaves, never per tick, so React does not re-render with the world.
import { defineStore } from './store';

export interface NearbyPlayer {
  readonly id: string;
  readonly name: string;
}

export interface NearbyState {
  readonly players: readonly NearbyPlayer[];
  setNearby(players: readonly NearbyPlayer[]): void;
  clear(): void;
}

function same(a: readonly NearbyPlayer[], b: readonly NearbyPlayer[]): boolean {
  return a.length === b.length && a.every((p, i) => p.id === b[i]!.id && p.name === b[i]!.name);
}

export const nearby = defineStore<NearbyState>((set, get) => ({
  players: [],
  setNearby: (players) => {
    const sorted = [...players].sort((x, y) => x.name.localeCompare(y.name));
    if (!same(get().players, sorted)) {
      set({ players: sorted });
    }
  },
  clear: () => {
    if (get().players.length > 0) {
      set({ players: [] });
    }
  },
}));
