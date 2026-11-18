// The match bar (pvp.md section 13): each side's rounds won as pips, the phase and its time counted down from what the server said, the enemy's names, and Leave match, which forfeits after a second press.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { roundsToWin } from '@/features/pvp/modes';
import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import type { GameSocket } from '@/net/socket';
import { MatchPhase } from '@/protocol/messages';
import { character } from '@/state/character';
import { pvp, sideOf } from '@/state/pvp';
import type { Fighter, Match } from '@/state/pvp';

import { timeLeft } from './BossBar';

export interface MatchBarProps {
  readonly socket: Pick<GameSocket, 'request'>;
}

export function MatchBar({ socket }: MatchBarProps) {
  const match = pvp.use((s) => s.match);
  const me = character.use((s) => s.character?.id ?? '');
  return match ? <Shown match={match} me={me} socket={socket} /> : null;
}

function Shown({
  match,
  me,
  socket,
}: {
  readonly match: Match;
  readonly me: string;
  readonly socket: Pick<GameSocket, 'request'>;
}) {
  const now = useNow(250);
  const [leaving, setLeaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const side = sideOf(match, me);
  const mine = match.wins[side - 1] ?? 0;
  const theirs = match.wins[2 - side] ?? 0;
  const enemies = match.sides[2 - side] ?? [];
  const need = roundsToWin(match.mode);
  const left = match.endsAt - now;
  const forfeit = async () => {
    try {
      await socket.request('pvpForfeit', {});
    } catch (err) {
      setProblem(problemOf(err));
    }
    setLeaving(false);
  };
  return (
    <>
      <div className="match-bar" role="group" aria-label="Match">
        <span className="match-side side-ally">
          <span>You</span>
          <Pips label="Your rounds" won={mine} of={need} />
        </span>
        <span className="match-clock">
          <span className="match-phase">{phaseName(match)}</span>
          <span className="match-time" title="Time left">
            {timeLeft(left)}
          </span>
        </span>
        <span className="match-side side-enemy">
          <Pips label="Their rounds" won={theirs} of={need} />
          {enemies.map((f) => (
            <Enemy key={f.id} fighter={f} />
          ))}
        </span>
        {match.phase !== MatchPhase.RESULT &&
          (leaving ? (
            <span className="match-leave">
              <span>Leave and lose?</span>
              <Button variant="danger" onClick={() => void forfeit()}>
                Leave
              </Button>
              <Button variant="quiet" onClick={() => setLeaving(false)}>
                Stay
              </Button>
            </span>
          ) : (
            <Button variant="quiet" onClick={() => setLeaving(true)}>
              Leave match
            </Button>
          ))}
        {problem && (
          <span className="error" role="alert">
            {problem}
          </span>
        )}
      </div>
      {match.phase === MatchPhase.COUNTDOWN && left > 0 && (
        <div className="match-count" aria-hidden="true">
          {Math.ceil(left / 1000)}
        </div>
      )}
    </>
  );
}

/** What the phase is, as the bar heads its time. */
function phaseName(m: Match): string {
  switch (m.phase) {
    case MatchPhase.PREP:
      return 'Get ready';
    case MatchPhase.COUNTDOWN:
      return `Round ${m.round} starts in`;
    case MatchPhase.FIGHT:
      return `Round ${m.round}`;
    case MatchPhase.BETWEEN:
      return 'Next round in';
    case MatchPhase.RESULT:
      return 'Back in';
    case MatchPhase.UNSPECIFIED:
      return '';
  }
}

function Pips({
  label,
  won,
  of,
}: {
  readonly label: string;
  readonly won: number;
  readonly of: number;
}) {
  return (
    <span className="match-pips" role="img" aria-label={`${label}: ${won} of ${of}`}>
      {Array.from({ length: of }, (_, i) => (
        <span key={i} className={i < won ? 'match-pip won' : 'match-pip'} />
      ))}
    </span>
  );
}

function Enemy({ fighter }: { readonly fighter: Fighter }) {
  return (
    <span className="match-enemy">
      {fighter.name}
      {fighter.bot && <span className="unit-frame-badge">BOT</span>}
    </span>
  );
}
