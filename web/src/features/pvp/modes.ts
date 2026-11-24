// The PvP modes the queue takes (pvp.md section 3), as the window, the cards and the bar name them.
import { maps } from '@/gamedata/maps';
import { pvp } from '@/gamedata/pvp';

export type Mode = (typeof pvp.modes)[number];

function modeById(id: string): Mode | undefined {
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

/** Whether the mode is a battle, fought for points and a score rather than rounds (pvp.md section 7); false for one this client doesn't know. */
export function isBattle(id: string): boolean {
  return modeById(id)?.best_of === null;
}

/** A battle's points' names by id, as its map names them; none for a mode this client doesn't know. */
export function pointNames(id: string): ReadonlyMap<string, string> {
  const map = maps.find((m) => m.id === modeById(id)?.map);
  return new Map(map?.points.map((p) => [p.id, p.name]) ?? []);
}

/** How many fight on a side in the mode, 1 for one this client doesn't know. */
export function teamOf(id: string): number {
  return modeById(id)?.team ?? 1;
}

/** A rank id as a name: silver is Silver. */
export function rankName(id: string): string {
  return id.charAt(0).toUpperCase() + id.slice(1);
}

/** A standing's record: wins, losses and draws. */
export function recordOf(s: {
  readonly wins: number;
  readonly losses: number;
  readonly draws: number;
}): string {
  return `${s.wins} W · ${s.losses} L · ${s.draws} D`;
}

/** m:ss of a wait, rounded down. */
export function waited(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
