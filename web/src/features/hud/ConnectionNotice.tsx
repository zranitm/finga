// A notice while the connection is in trouble (page 7, Connection loss):
// shown on reconnecting, gone once connected.
import { connection } from '@/state/connection';

export function ConnectionNotice() {
  const status = connection.use((s) => s.status);
  const attempt = connection.use((s) => s.attempt);
  if (status !== 'reconnecting') {
    return null;
  }
  return (
    <div className="notice-banner" role="status">
      Reconnecting{attempt > 1 ? ` (attempt ${attempt})` : ''}…
    </div>
  );
}
