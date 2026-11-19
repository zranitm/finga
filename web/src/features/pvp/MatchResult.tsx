// The result screen (pvp.md section 13): the outcome, the rounds, the rating's change and any new rank; it stays, through the trip back, till the player closes it.
import { Button } from '@/components/Button';
import { MatchOutcome } from '@/protocol/messages';
import { pvp } from '@/state/pvp';
import type { RatingChange } from '@/state/pvp';

import { modeName } from './modes';

const TITLE: Readonly<Record<MatchOutcome, string>> = {
  [MatchOutcome.UNSPECIFIED]: 'Match over',
  [MatchOutcome.WIN]: 'Victory',
  [MatchOutcome.LOSS]: 'Defeat',
  [MatchOutcome.DRAW]: 'Draw',
};

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
        {TITLE[outcome.result]}
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
  const delta = r.after - r.before;
  return (
    <>
      <span>
        Rating {r.before} → {r.after} ({delta >= 0 ? `+${delta}` : `−${-delta}`})
      </span>
      {r.placementLeft > 0 ? (
        <span className="muted">
          {r.placementLeft} placement {r.placementLeft === 1 ? 'match' : 'matches'} left
        </span>
      ) : (
        r.rankAfter !== r.rankBefore && <span>New rank: {rankName(r.rankAfter)}</span>
      )}
    </>
  );
}

/** A rank id as a name: silver is Silver. */
function rankName(id: string): string {
  return id.charAt(0).toUpperCase() + id.slice(1);
}
