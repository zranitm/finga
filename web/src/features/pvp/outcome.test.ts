import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { MatchEndedSchema, MatchOutcome, WelcomeSchema } from '@/protocol/messages';

import { leaverLine, missedLines } from './outcome';

const rating = (after: number, placementLeft: number, rankAfter = 'bronze') => ({
  before: 1000,
  after,
  rankBefore: 'bronze',
  rankAfter,
  placementLeft,
});

describe('leaverLine', () => {
  it.each([
    [
      rating(968, 4),
      'Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32). 4 placement matches left.',
    ],
    [
      rating(968, 1),
      'Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32). 1 placement match left.',
    ],
    [
      rating(968, 0, 'iron'),
      'Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32). New rank: Iron.',
    ],
    [rating(1000, 0), 'Your 3v3 match ended: Defeat. Rating 1000 → 1000 (+0).'],
    [undefined, 'Your 3v3 match ended: Defeat.'],
  ])('says the mode, the result and the rating’s move: %o', (r, said) => {
    const d = create(MatchEndedSchema, {
      mode: 'team',
      result: MatchOutcome.LOSS,
      left: true,
      rating: r,
    });
    expect(leaverLine(d)).toBe(said);
  });
});

describe('missedLines', () => {
  const now = 1_764_000_000_000n;

  it('says each missed match’s mode, how long ago by the server’s clock and how it went, a leaver’s as a loss, then how many more', () => {
    const d = create(WelcomeSchema, {
      serverTime: now,
      missedMatches: [
        { mode: 'duel', result: MatchOutcome.LOSS, left: true, endedAt: now - 20_000n },
        { mode: 'team', result: MatchOutcome.WIN, endedAt: now - 2n * 3_600_000n },
        { mode: 'battle', result: MatchOutcome.DRAW, endedAt: now - 3n * 86_400_000n },
        { mode: 'duel', result: MatchOutcome.LOSS, endedAt: now - 12n * 60_000n },
      ],
      missedMore: 3,
    });
    expect(missedLines(d)).toEqual([
      'Your 1v1 match ended moments ago, while you were away: it counted as a loss.',
      'Your 3v3 match ended 2 h ago, while you were away: Victory.',
      'Your 15v15 match ended 3 days ago, while you were away: Draw.',
      'Your 1v1 match ended 12 min ago, while you were away: Defeat.',
      '…and 3 more matches ended while you were away.',
    ]);
  });

  it.each([
    [0, []],
    [1, ['…and 1 more match ended while you were away.']],
  ])('with no match listed and %i more, says %o', (more, said) => {
    expect(missedLines(create(WelcomeSchema, { missedMore: more }))).toEqual(said);
  });

  it.each([
    [0n, 'moments ago'],
    [59_999n, 'moments ago'],
    [60_000n, '1 min ago'],
    [3_599_999n, '59 min ago'],
    [3_600_000n, '1 h ago'],
    [48n * 3_600_000n - 1n, '47 h ago'],
    [48n * 3_600_000n, '2 days ago'],
  ])('says a match %i ms before the Welcome ended %s', (before, said) => {
    const d = create(WelcomeSchema, {
      serverTime: now,
      missedMatches: [{ mode: 'duel', result: MatchOutcome.WIN, endedAt: now - before }],
    });
    expect(missedLines(d)).toEqual([`Your 1v1 match ended ${said}, while you were away: Victory.`]);
  });
});
