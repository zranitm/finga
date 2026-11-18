// The Arena window (pvp.md section 13, Play): a card for each mode with Queue or Leave queue; the server's refusal shows under them.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { pvp as data } from '@/gamedata/pvp';
import type { GameSocket } from '@/net/socket';
import { pvp } from '@/state/pvp';

import { waited } from './modes';
import type { Mode } from './modes';

export interface ArenaWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

export function ArenaWindow({ socket, onClose }: ArenaWindowProps) {
  const queued = pvp.use((s) => s.queued);
  const inMatch = pvp.use((s) => s.match !== null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (request: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(null);
    try {
      await request();
    } catch (err) {
      setProblem(problemOf(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <WorldWindow name="arena" title="Arena" onClose={onClose} initialX={620} initialY={120}>
      <div className="arena">
        <p className="muted">
          Everyone fights at level {data.kit_level} with their class’s set stats and gear, and every
          skill fully learned.
        </p>
        <ul className="arena-modes" aria-label="Modes">
          {data.modes.map((m) => (
            <li key={m.id} className="arena-mode" aria-label={m.name}>
              <div className="arena-mode-head">
                <strong>{m.name}</strong>
                <span className="muted">{m.rated ? 'Rated' : 'Unrated'}</span>
              </div>
              <span className="muted">{describe(m)}</span>
              {queued?.mode === m.id ? (
                <div className="row">
                  <Waiting since={queued.since} />
                  <Button
                    variant="quiet"
                    disabled={busy}
                    onClick={() => void run(() => socket.request('pvpLeaveQueue', {}))}
                  >
                    Leave queue
                  </Button>
                </div>
              ) : (
                <Button
                  disabled={busy || inMatch || queued !== null}
                  onClick={() => void run(() => socket.request('pvpQueue', { mode: m.id }))}
                >
                  Queue
                </Button>
              )}
            </li>
          ))}
        </ul>
        {inMatch && <p className="muted">You’re in a match.</p>}
        {queued && <p className="muted">Play on while you wait: the match finds you.</p>}
        {problem && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}
      </div>
    </WorldWindow>
  );
}

/** How many a side and the rounds a match is best of. */
function describe(m: Mode): string {
  return `${m.team} against ${m.team}, best of ${m.best_of} rounds.`;
}

function Waiting({ since }: { readonly since: number }) {
  const now = useNow(1000);
  return (
    <span className="arena-waiting" aria-label={`Waiting ${waited(now - since)}`}>
      Waiting {waited(now - since)}
    </span>
  );
}
