// A match's outcome in words (pvp.md section 13): the result card's title and rating, and the General line a team match's leaver gets instead of the card.
import { MatchOutcome } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { RatingChange } from '@/state/pvp';

import { modeName, rankName } from './modes';

const TITLE: Readonly<Record<MatchOutcome, string>> = {
  [MatchOutcome.UNSPECIFIED]: 'Match over',
  [MatchOutcome.WIN]: 'Victory',
  [MatchOutcome.LOSS]: 'Defeat',
  [MatchOutcome.DRAW]: 'Draw',
};

export function outcomeTitle(result: MatchOutcome): string {
  return TITLE[result];
}

/** "Rating 1000 → 1032 (+32)". */
export function ratingMoved(r: RatingChange): string {
  const delta = r.after - r.before;
  return `Rating ${r.before} → ${r.after} (${delta >= 0 ? `+${delta}` : `−${-delta}`})`;
}

/** "3 placement matches left", or the new rank once placed, or nothing. */
export function ratingNote(r: RatingChange): string | null {
  if (r.placementLeft > 0) {
    return `${r.placementLeft} placement ${r.placementLeft === 1 ? 'match' : 'matches'} left`;
  }
  return r.rankAfter !== r.rankBefore ? `New rank: ${rankName(r.rankAfter)}` : null;
}

/** "Your 3v3 match ended: Defeat. Rating 1000 → 968 (−32). 4 placement matches left." */
export function leaverLine(d: Data<'matchEnded'>): string {
  const said = [`Your ${modeName(d.mode)} match ended: ${TITLE[d.result]}.`];
  if (d.rating) {
    said.push(`${ratingMoved(d.rating)}.`);
    const note = ratingNote(d.rating);
    if (note) {
      said.push(`${note}.`);
    }
  }
  return said.join(' ');
}
