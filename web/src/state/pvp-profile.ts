// The character's PvP standings (pvp.md section 10) as the server last told them: each rated mode's rating, rank, placement, record and place.
import { defineStore } from './store';

/** The character's standing in a rated mode, as the profile gives it. */
export interface PvpStanding {
  readonly mode: string;
  readonly shown: number;
  readonly rank: string;
  /** Placement matches still to play, 0 once placed. */
  readonly placement_left: number;
  readonly wins: number;
  readonly losses: number;
  readonly draws: number;
  /** One past those placed above, 0 before placement. */
  readonly place: number;
}

export interface PvpProfileState {
  /** Null till the first read. */
  readonly standings: readonly PvpStanding[] | null;
  /** Counts the asks for a fresh read, which the profile's watcher answers. */
  readonly asked: number;
  apply(standings: readonly PvpStanding[]): void;
  ask(): void;
}

export const pvpProfile = defineStore<PvpProfileState>((set, get) => ({
  standings: null,
  asked: 0,
  apply: (standings) => {
    set({ standings });
  },
  ask: () => {
    set({ asked: get().asked + 1 });
  },
}));

/** The character's standing in mode, if read. */
export function standingIn(
  standings: readonly PvpStanding[] | null,
  mode: string,
): PvpStanding | undefined {
  return standings?.find((s) => s.mode === mode);
}
