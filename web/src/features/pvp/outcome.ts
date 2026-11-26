// A match's outcome in words (pvp.md section 13): the result card's title and rating, what a team match's leaver hears as they go and the General line they get instead of the card, and a Welcome's lines for matches that ended while the player was away.
import { rankName } from '@/progression/names';
import { MatchOutcome } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { RatingChange } from '@/state/pvp';

import { isRated, modeName } from './modes';

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

/** What leaving a match of mode under way costs, said as the player goes: a rated mode's rating moves only once the match ends. */
export function quitLine(mode: string): string {
  const loss = 'You left the match: it counts as a loss.';
  return isRated(mode) ? `${loss} Your rating updates when the match ends.` : loss;
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

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

/** How long ago something ms ago was: "moments ago", "12 min ago", "5 h ago", "3 days ago". */
function ago(ms: number): string {
  if (ms < MINUTE) {
    return 'moments ago';
  }
  if (ms < HOUR) {
    return `${Math.floor(ms / MINUTE)} min ago`;
  }
  if (ms < 48 * HOUR) {
    return `${Math.floor(ms / HOUR)} h ago`;
  }
  return `${Math.floor(ms / (24 * HOUR))} days ago`;
}

/** "Your 1v1 match ended 2 h ago, while you were away: it counted as a loss.", one a match, timed by the server's clock, then "…and 3 more matches ended while you were away." */
export function missedLines(d: Data<'welcome'>): string[] {
  const said = d.missedMatches.map(
    (m) =>
      `Your ${modeName(m.mode)} match ended ${ago(Number(d.serverTime - m.endedAt))}, while you were away: ${m.left ? 'it counted as a loss' : TITLE[m.result]}.`,
  );
  if (d.missedMore > 0) {
    said.push(
      `…and ${d.missedMore} more ${d.missedMore === 1 ? 'match' : 'matches'} ended while you were away.`,
    );
  }
  return said;
}
