// Incoming party invites (C13): who asks, Accept and Decline through
// PartyRespond, and a 30 s countdown after which the invite goes by
// itself (the server expires it too).
import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import type { GameSocket } from '@/net/socket';
import { party } from '@/state/party';
import type { Invite } from '@/state/party';

export const INVITE_SECONDS = 30;

export interface InviteToastProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly now?: () => number;
}

export function InviteToasts({ socket, now = Date.now }: InviteToastProps) {
  const invites = party.use((s) => s.invites);
  if (invites.length === 0) {
    return null;
  }
  return (
    <div className="invites">
      {invites.map((inv) => (
        <InviteItem key={inv.invite_id} invite={inv} socket={socket} now={now} />
      ))}
    </div>
  );
}

function InviteItem({
  invite,
  socket,
  now,
}: {
  readonly invite: Invite;
  readonly socket: Pick<GameSocket, 'request'>;
  readonly now: () => number;
}) {
  const [left, setLeft] = useState(() => secondsLeft(invite, now()));
  useEffect(() => {
    const id = setInterval(() => {
      const s = secondsLeft(invite, now());
      setLeft(s);
      if (s <= 0) {
        party.getState().dismissInvite(invite.invite_id);
      }
    }, 1000);
    return () => clearInterval(id);
  }, [invite, now]);

  const respond = (accept: boolean) => {
    party.getState().dismissInvite(invite.invite_id);
    void socket.request('PartyRespond', { invite_id: invite.invite_id, accept }).catch(() => {
      // The server's reason is not worth a toast here: the invite may simply have expired.
    });
  };

  return (
    <div className="invite" role="status">
      <span>
        <strong>{invite.from_name}</strong> invites you to a party ({left}s)
      </span>
      <Button onClick={() => respond(true)}>Accept</Button>
      <Button variant="quiet" onClick={() => respond(false)}>
        Decline
      </Button>
    </div>
  );
}

function secondsLeft(invite: Invite, nowMs: number): number {
  return Math.max(0, Math.ceil((invite.at + INVITE_SECONDS * 1000 - nowMs) / 1000));
}
