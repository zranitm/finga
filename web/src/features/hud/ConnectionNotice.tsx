import { connection } from '@/state/connection';

export function ConnectionNotice() {
  const status = connection.use((s) => s.status);
  const attempt = connection.use((s) => s.attempt);
  const stalled = connection.use((s) => s.worldStalled);
  if (status === 'reconnecting') {
    return (
      <div className="notice-banner" role="status">
        Reconnecting{attempt > 1 ? ` (attempt ${attempt})` : ''}…
      </div>
    );
  }
  if (status === 'connected' && stalled) {
    return (
      <div className="notice-banner" role="status">
        Waiting for the server…
      </div>
    );
  }
  return null;
}
