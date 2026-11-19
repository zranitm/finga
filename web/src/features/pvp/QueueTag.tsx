// The queue tag by the minimap (pvp.md section 13): the mode, the party members queued with the player, and the time waited, counted from when the queue said, with a button to leave.
import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import type { GameSocket } from '@/net/socket';
import { pvp } from '@/state/pvp';
import type { Queued } from '@/state/pvp';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';

import { useWith } from './mates';
import { modeName, waited } from './modes';

export interface QueueTagProps {
  readonly socket: Pick<GameSocket, 'request'>;
}

export function QueueTag({ socket }: QueueTagProps) {
  const queued = pvp.use((s) => s.queued);
  return queued ? <Shown queued={queued} socket={socket} /> : null;
}

function Shown({
  queued,
  socket,
}: {
  readonly queued: Queued;
  readonly socket: Pick<GameSocket, 'request'>;
}) {
  const minimap = settings.use((s) => s.showMinimap);
  const now = useNow(1000);
  const name = modeName(queued.mode);
  const mates = useWith(queued.members);
  const leave = () =>
    void socket
      .request('pvpLeaveQueue', {})
      .catch((err: unknown) => toasts.getState().push(problemOf(err)));
  return (
    <div
      className={minimap ? 'queue-tag' : 'queue-tag low'}
      role="status"
      aria-label={`In the ${name} queue`}
    >
      <span>{name} queue</span>
      {mates && <span className="muted">{mates}</span>}
      <span className="queue-tag-time">{waited(now - queued.since)}</span>
      <button
        type="button"
        className="queue-tag-leave"
        aria-label="Leave the queue"
        title="Leave the queue"
        onClick={leave}
      >
        ✕
      </button>
    </div>
  );
}
