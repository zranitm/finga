// Each mode's leaderboard, read at most once in the time the server serves one read of it, so flipping between the Leaderboard's modes spends no calls; a read under way is shared, and a failed one is tried again at the next ask.
import type { Leaderboard, RestClient } from '@/net/rest';

/** How long the server serves one read of a board (pvp.md section 10). */
export const BOARD_FOR_MS = 30_000;

export type BoardReader = (mode: string) => Promise<Leaderboard>;

export function boardReader(
  rest: Pick<RestClient, 'getLeaderboard'>,
  now: () => number = () => performance.now(),
): BoardReader {
  const reads = new Map<string, { readonly at: number; readonly board: Promise<Leaderboard> }>();
  return (mode) => {
    const last = reads.get(mode);
    if (last && now() - last.at < BOARD_FOR_MS) {
      return last.board;
    }
    const board = rest.getLeaderboard(mode);
    reads.set(mode, { at: now(), board });
    board.catch(() => reads.delete(mode));
    return board;
  };
}
