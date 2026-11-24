// The Arena window (pvp.md section 13). Play: a card for each mode with the player's standing in it, and Queue or Leave queue; the server's refusal shows under them. Leaderboard: each rated mode's board. Opening it reads the profile afresh.
import { useEffect, useState } from 'react';

import { Button } from '@/components/Button';
import { Tabs } from '@/components/Tabs';
import { problemOf } from '@/features/shared/problem';
import { useNow } from '@/features/shared/useNow';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { pvp as data } from '@/gamedata/pvp';
import type { PvpStanding } from '@/net/rest';
import type { GameSocket } from '@/net/socket';
import { pvp } from '@/state/pvp';
import { pvpProfile, standingIn } from '@/state/pvp-profile';

import type { BoardReader } from './boards';
import { Leaderboard } from './Leaderboard';
import { rankName, recordOf, waited } from './modes';
import type { Mode } from './modes';

export interface ArenaWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly board: BoardReader;
  readonly onClose: () => void;
}

type ArenaTab = 'play' | 'board';

export function ArenaWindow({ socket, board, onClose }: ArenaWindowProps) {
  const [tab, setTab] = useState<ArenaTab>('play');
  useEffect(() => pvpProfile.getState().ask(), []);
  return (
    <WorldWindow name="arena" title="Arena" onClose={onClose} initialX={620} initialY={120}>
      <div className="arena">
        <Tabs
          label="Arena"
          tabs={[
            { id: 'play', name: 'Play' },
            { id: 'board', name: 'Leaderboard' },
          ]}
          chosen={tab}
          onChoose={setTab}
        />
        {tab === 'play' ? <Play socket={socket} /> : <Leaderboard board={board} />}
      </div>
    </WorldWindow>
  );
}

function Play({ socket }: { readonly socket: Pick<GameSocket, 'request'> }) {
  const queued = pvp.use((s) => s.queued);
  const inMatch = pvp.use((s) => s.match !== null);
  const standings = pvpProfile.use((s) => s.standings);
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
    <>
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
            {m.rated ? (
              <Standing standing={standingIn(standings, m.id)} />
            ) : (
              <Record standing={standingIn(standings, m.id)} />
            )}
            {queued?.mode === m.id ? (
              <div className="row">
                <Waiting since={queued.since} room={queued.waitingRoom} />
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
    </>
  );
}

/** The player's standing in a rated mode: their rating, rank and place once placed, else how far through placement they are, and their record; nothing till the profile's read. */
function Standing({ standing: s }: { readonly standing: PvpStanding | undefined }) {
  if (!s) {
    return null;
  }
  return (
    <div className="arena-standing" aria-label="Your standing">
      {s.placement_left > 0 ? (
        <span>
          Placing ({data.placement - s.placement_left} of {data.placement})
        </span>
      ) : (
        <span>
          <strong>{s.shown}</strong> {rankName(s.rank)}
          {s.place > 0 && <span className="muted"> · #{s.place}</span>}
        </span>
      )}
      <span className="muted">{recordOf(s)}</span>
    </div>
  );
}

/** An unrated mode's record alone; nothing till the profile's read. */
function Record({ standing: s }: { readonly standing: PvpStanding | undefined }) {
  return s ? (
    <div className="arena-standing" aria-label="Your record">
      <span className="muted">{recordOf(s)}</span>
    </div>
  ) : null;
}

/** How many a side, and the rounds a match is best of, or a battle's goal and length. */
function describe(m: Mode): string {
  if (m.best_of === null) {
    const goal = data.battle.goal.toLocaleString('en-US');
    return `${m.team} against ${m.team}: first to ${goal} points, or the most in ${data.battle.length_ms / 60_000} minutes.`;
  }
  return `${m.team} against ${m.team}, best of ${m.best_of} rounds.`;
}

/** The time waited, and while the match waits for room for its bots, that it does. */
function Waiting({ since, room }: { readonly since: number; readonly room: boolean }) {
  const now = useNow(1000);
  const text = `Waiting${room ? ' for room' : ''} ${waited(now - since)}`;
  return (
    <span className="arena-waiting" aria-label={text}>
      {text}
    </span>
  );
}
