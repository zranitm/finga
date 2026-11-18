// The PvP modes the queue takes (pvp.md section 3), as the window, the cards and the bar name them.
import { pvp } from '@/gamedata/pvp';

export type Mode = (typeof pvp.modes)[number];

export function modeById(id: string): Mode | undefined {
  return pvp.modes.find((m) => m.id === id);
}

/** The mode's name, or its id for one this client doesn't know. */
export function modeName(id: string): string {
  return modeById(id)?.name ?? id;
}

/** How many rounds a side needs to win the match, 0 for a mode without rounds. */
export function roundsToWin(id: string): number {
  const best = modeById(id)?.best_of ?? 0;
  return best > 0 ? Math.floor(best / 2) + 1 : 0;
}

/** m:ss of a wait, rounded down. */
export function waited(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
