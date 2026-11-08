// Membership changes only on PartyState, never on a command's Response.
import { useState } from 'react';

import { Bar } from '@/components/Bar';
import { Button } from '@/components/Button';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { nearby } from '@/state/nearby';
import { isMember, party } from '@/state/party';

export const MAX_PARTY = 4;

export interface PartyWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly me: string;
  readonly onClose: () => void;
}

export function PartyWindow({ socket, me, onClose }: PartyWindowProps) {
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
    <WorldWindow name="party" title="Party" onClose={onClose} initialX={740} initialY={80}>
      <div className="party">
        {current ? (
          <ul className="party-members" aria-label="Members">
            {current.members.map((m) => (
              <li key={m.characterId} className={m.online ? '' : 'party-offline'}>
                <div className="party-member-head">
                  <strong>
                    {m.name}
                    {m.characterId === current.leader ? ' (leader)' : ''}
                  </strong>
                  <span>
                    Lv. {m.level} · {m.mapId}
                    {m.online ? '' : ' · offline'}
                  </span>
                </div>
                <Bar label={`${m.name} HP`} value={m.hp} max={m.maxHp} kind="hp" />
                {leader && m.characterId !== me && (
                  <div className="row">
                    <Button
                      variant="quiet"
                      disabled={busy}
                      onClick={() =>
                        void run(() =>
                          socket.request('partyPromote', { characterId: m.characterId }),
                        )
                      }
                    >
                      Promote
                    </Button>
                    <Button
                      variant="danger"
                      disabled={busy}
                      onClick={() =>
                        void run(() => socket.request('partyKick', { characterId: m.characterId }))
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
            onClick={() => void run(() => socket.request('partyLeave', {}))}
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
                    void run(() => socket.request('partyInvite', { characterId: p.id }))
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
    </WorldWindow>
  );
}
