import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { MatchEndedSchema, MatchOutcome } from '@/protocol/messages';

import { leaverLine } from './outcome';

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
