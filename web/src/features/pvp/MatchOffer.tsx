// The match-found card (pvp.md section 13): the mode and its bots, the party members queued with the player, Accept and Decline, and the seconds left counted down from the offer.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import type { GameSocket } from '@/net/socket';
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';
import type { Offer } from '@/state/pvp';

import { useWith } from './mates';
import { modeName } from './modes';

export interface MatchOfferProps {
  readonly socket: Pick<GameSocket, 'request'>;
}

export function MatchOffer({ socket }: MatchOfferProps) {
  const offer = pvp.use((s) => s.offer);
  return offer ? <Shown key={offer.id} offer={offer} socket={socket} /> : null;
}

function Shown({
  offer,
  socket,
}: {
  readonly offer: Offer;
  readonly socket: Pick<GameSocket, 'request'>;
}) {
  const now = useNow(250);
  const dead = character.use((s) => s.dead);
  const mates = useWith(pvp.use((s) => s.queued?.members ?? none));
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState<string | null>(null); // the offer accepted: its answer comes once the match opens
  const [problem, setProblem] = useState<string | null>(null);
  const left = Math.ceil((offer.until - now) / 1000);
  if (left <= 0) {
    return null; // the queue says what came of it
  }
  const answer = async (accept: boolean) => {
    setBusy(true);
    setProblem(null);
    if (accept) {
      setAccepted(offer.id);
    }
    try {
      await socket.request('pvpAnswer', { offer: offer.id, accept });
    } catch (err) {
      setAccepted(null);
      setProblem(problemOf(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="match-offer" role="alertdialog" aria-label="Match found">
      <strong>Match found</strong>
      <span>
        {modeName(offer.mode)} · {botsIn(offer.bots)}
      </span>
      {mates && <span className="muted">{mates}</span>}
      <span className="match-offer-time" aria-label={`${left} seconds left`}>
        {left}s
      </span>
      {accepted === offer.id ? (
        <span className="muted">Waiting for the others…</span>
      ) : (
        <div className="row">
          <Button disabled={busy || dead} onClick={() => void answer(true)}>
            Accept
          </Button>
          <Button variant="quiet" disabled={busy} onClick={() => void answer(false)}>
            Decline
          </Button>
        </div>
      )}
      {dead && accepted !== offer.id && (
        <span className="muted">Get back on your feet to accept.</span>
      )}
      {problem && (
        <span className="error" role="alert">
          {problem}
        </span>
      )}
    </div>
  );
}

const none: readonly string[] = [];

function botsIn(n: number): string {
  return n === 0 ? 'no bots' : n === 1 ? '1 bot' : `${n} bots`;
}
