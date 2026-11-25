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
  it('says each missed match’s mode and how it went, a leaver’s as a loss, then how many more', () => {
    const d = create(WelcomeSchema, {
      missedMatches: [
        { mode: 'duel', result: MatchOutcome.LOSS, left: true },
        { mode: 'team', result: MatchOutcome.WIN },
        { mode: 'battle', result: MatchOutcome.DRAW },
        { mode: 'duel', result: MatchOutcome.LOSS },
      ],
      missedMore: 3,
    });
    expect(missedLines(d)).toEqual([
      'Your 1v1 match ended while you were away: it counted as a loss.',
      'Your 3v3 match ended while you were away: Victory.',
      'Your 15v15 match ended while you were away: Draw.',
      'Your 1v1 match ended while you were away: Defeat.',
      '…and 3 more matches ended while you were away.',
    ]);
  });

  it.each([
    [0, []],
    [1, ['…and 1 more match ended while you were away.']],
  ])('with no match listed and %i more, says %o', (more, said) => {
    expect(missedLines(create(WelcomeSchema, { missedMore: more }))).toEqual(said);
  });
});
