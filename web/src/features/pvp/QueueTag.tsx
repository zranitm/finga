// The queue tag by the minimap (pvp.md section 13): the mode and the time waited, counted from when the queue said, with a button to leave.
import { useNow } from '@/features/shared/useNow';
import type { GameSocket } from '@/net/socket';
import { pvp } from '@/state/pvp';
import { settings } from '@/state/settings';

import { modeName, waited } from './modes';

export interface QueueTagProps {
  readonly socket: Pick<GameSocket, 'request'>;
}

export function QueueTag({ socket }: QueueTagProps) {
  const queued = pvp.use((s) => s.queued);
  const minimap = settings.use((s) => s.showMinimap);
  const now = useNow(1000);
  if (!queued) {
    return null;
  }
  const name = modeName(queued.mode);
  return (
    <div
      className={minimap ? 'queue-tag' : 'queue-tag low'}
      role="status"
      aria-label={`In the ${name} queue`}
    >
      <span>{name} queue</span>
      <span className="queue-tag-time">{waited(now - queued.since)}</span>
      <button
        type="button"
        className="queue-tag-leave"
        aria-label="Leave the queue"
        title="Leave the queue"
        onClick={() => void socket.request('pvpLeaveQueue', {}).catch(() => undefined)}
      >
        ✕
      </button>
    </div>
  );
}
