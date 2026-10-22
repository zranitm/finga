import { describe, expect, it } from 'vitest';

import { backoff } from './backoff';

const opts = { baseMs: 500, capMs: 10_000 };

describe('backoff', () => {
  it.each([
    { name: 'the first retry at the top of its window', attempt: 0, random: 0.999, want: 499 },
    { name: 'the first retry at the bottom', attempt: 0, random: 0, want: 0 },
    { name: 'the window doubles each attempt', attempt: 2, random: 0.5, want: 1000 },
    { name: 'the window is capped', attempt: 10, random: 0.999, want: 9990 },
    { name: 'the cap holds for any later attempt', attempt: 40, random: 0.5, want: 5000 },
  ])('$name', ({ attempt, random, want }) => {
    expect(backoff(attempt, { ...opts, random: () => random })).toBe(want);
  });

  it('never leaves the jitter window with the real random source', () => {
    for (let attempt = 0; attempt < 8; attempt++) {
      const bound = Math.min(opts.capMs, opts.baseMs * 2 ** attempt);
      for (let k = 0; k < 200; k++) {
        const d = backoff(attempt, opts);
        expect(d).toBeGreaterThanOrEqual(0);
        expect(d).toBeLessThan(bound);
      }
    }
  });

  it.each([-1, 1.5])('refuses attempt %s', (attempt) => {
    expect(() => backoff(attempt, opts)).toThrow(RangeError);
  });
});
