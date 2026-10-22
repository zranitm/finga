// The party window (C13): invite a player on the same map from the
// nearby list (PartyInvite takes a character id), the members with level,
// HP, map and online state from PartyState, leave, and kick and promote
// for the leader. Every command carries a request ID and shows the
// server's reason on failure; membership changes only on PartyState.
import { useState } from 'react';

import { Bar } from '@/components/Bar';
import { Button } from '@/components/Button';
import { Window } from '@/components/Window';
import type { GameSocket } from '@/net/socket';
import { nearby } from '@/state/nearby';
import { isMember, party } from '@/state/party';

export const MAX_PARTY = 4;

export interface PartyWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  /** The local character's id, to tell the leader's controls from a member's. */
  readonly me: string;
  readonly onClose: () => void;
  readonly onFocusChange?: (focused: boolean) => void;
}

export function PartyWindow({ socket, me, onClose, onFocusChange }: PartyWindowProps) {
  const current = party.use((s) => s.party);
  const players = nearby.use((s) => s.players);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (send: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(null);
    try {
      await send();
    } catch (err) {
      setProblem(err instanceof Error ? err.message : 'The server refused.');
    } finally {
      setBusy(false);
    }
  };

  const leader = current !== null && current.leader === me;
  const full = current !== null && current.members.length >= MAX_PARTY;
  const invitable = players.filter((p) => !isMember(current, p.id));

  return (
    <Window
      title="Party"
      onClose={onClose}
      {...(onFocusChange && { onFocusChange })}
      initialX={640}
      initialY={80}
    >
      <div className="party">
        {current ? (
          <ul className="party-members" aria-label="Members">
            {current.members.map((m) => (
              <li key={m.character_id} className={m.online ? '' : 'party-offline'}>
                <div className="party-member-head">
                  <strong>
                    {m.name}
                    {m.character_id === current.leader ? ' (leader)' : ''}
                  </strong>
                  <span>
                    Lv. {m.level} · {m.map_id}
                    {m.online ? '' : ' · offline'}
                  </span>
                </div>
                <Bar label={`${m.name} HP`} value={m.hp} max={m.max_hp} kind="hp" />
                {leader && m.character_id !== me && (
                  <div className="row">
                    <Button
                      variant="quiet"
                      disabled={busy}
                      onClick={() =>
                        void run(() =>
                          socket.request('PartyPromote', { character_id: m.character_id }),
                        )
                      }
                    >
                      Promote
                    </Button>
                    <Button
                      variant="danger"
                      disabled={busy}
                      onClick={() =>
                        void run(() =>
                          socket.request('PartyKick', { character_id: m.character_id }),
                        )
                      }
                    >
                      Kick
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <p>You are not in a party.</p>
        )}
        {current && (
          <Button
            variant="quiet"
            disabled={busy}
            onClick={() => void run(() => socket.request('PartyLeave', {}))}
          >
            Leave party
          </Button>
        )}
        <h3>Nearby</h3>
        {invitable.length === 0 ? (
          <p className="muted">Nobody else on this map.</p>
        ) : (
          <ul className="party-nearby" aria-label="Nearby players">
            {invitable.map((p) => (
              <li key={p.id}>
                <span>{p.name}</span>
                <Button
                  variant="quiet"
                  disabled={busy || full}
                  onClick={() =>
                    void run(() => socket.request('PartyInvite', { character_id: p.id }))
                  }
                >
                  Invite
                </Button>
              </li>
            ))}
          </ul>
        )}
        {full && <p className="notice">The party is full ({MAX_PARTY}).</p>}
        {problem && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}
      </div>
    </Window>
  );
}
