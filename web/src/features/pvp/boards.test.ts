import { describe, expect, it, vi } from 'vitest';

import type { Leaderboard } from '@/net/rest';

import { BOARD_FOR_MS, boardReader } from './boards';

function harness(answer: (mode: string) => Promise<Leaderboard>) {
  let t = 0;
  const getLeaderboard = vi.fn(answer);
  const read = boardReader({ getLeaderboard }, () => t);
  return { read, getLeaderboard, at: (ms: number) => (t = ms) };
}

const empty = (mode: string) => Promise.resolve({ mode, rows: [] });

describe('the boards', () => {
  it('quick flips between the modes read each mode once, and again once the server would have', async () => {
    const h = harness(empty);
    for (const mode of ['duel', 'team', 'duel', 'team', 'duel']) {
      await h.read(mode);
    }
    expect(h.getLeaderboard.mock.calls).toEqual([['duel'], ['team']]);
    h.at(BOARD_FOR_MS - 1);
    await h.read('duel');
    expect(h.getLeaderboard).toHaveBeenCalledTimes(2);
    h.at(BOARD_FOR_MS);
    await h.read('duel');
    expect(h.getLeaderboard.mock.calls.at(-1)).toEqual(['duel']);
    expect(h.getLeaderboard).toHaveBeenCalledTimes(3);
  });

  it('asks while a read is under way share it', async () => {
    let answer: (b: Leaderboard) => void = () => undefined;
    const h = harness(() => new Promise((r) => (answer = r)));
    const both = Promise.all([h.read('duel'), h.read('duel')]);
    answer({ mode: 'duel', rows: [] });
    expect(await both).toEqual([
      { mode: 'duel', rows: [] },
      { mode: 'duel', rows: [] },
    ]);
    expect(h.getLeaderboard).toHaveBeenCalledTimes(1);
  });

  it('a failed read is tried again at the next ask', async () => {
    const answers = [() => Promise.reject(new Error('down')), () => empty('duel')];
    const h = harness(() => answers.shift()!());
    await expect(h.read('duel')).rejects.toThrow('down');
    await expect(h.read('duel')).resolves.toEqual({ mode: 'duel', rows: [] });
    expect(h.getLeaderboard).toHaveBeenCalledTimes(2);
  });
});
