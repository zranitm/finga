// The result screen (pvp.md section 13): the outcome, the rounds, the rating's change and any new rank; it stays, through the trip back, till the player closes it.
import { Button } from '@/components/Button';
import { MatchOutcome } from '@/protocol/messages';
import { pvp } from '@/state/pvp';
import type { RatingChange } from '@/state/pvp';

import { modeName } from './modes';
import { outcomeTitle, ratingMoved, ratingNote } from './outcome';

export function MatchResult() {
  const outcome = pvp.use((s) => s.outcome);
  if (!outcome) {
    return null;
  }
  const [mine, theirs] = outcome.rounds;
  return (
    <div className="match-result" role="dialog" aria-label="Match result">
      <span className="muted">{modeName(outcome.mode)}</span>
      <strong className={`match-result-title result-${MatchOutcome[outcome.result].toLowerCase()}`}>
        {outcomeTitle(outcome.result)}
      </strong>
      <span>
        Rounds {mine} – {theirs}
      </span>
      {outcome.rating && <Rating change={outcome.rating} />}
      <Button onClick={() => pvp.getState().dismissOutcome()}>Return</Button>
    </div>
  );
}

function Rating({ change: r }: { readonly change: RatingChange }) {
  const note = ratingNote(r);
  return (
    <>
      <span>{ratingMoved(r)}</span>
      {note && <span className={r.placementLeft > 0 ? 'muted' : undefined}>{note}</span>}
    </>
  );
}
