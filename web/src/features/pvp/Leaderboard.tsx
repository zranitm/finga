// The Arena window's Leaderboard (pvp.md section 13): a rated mode's top 100 placed, the player's own row marked, and under the list their own place, or how far their placement is.
import { useEffect, useState } from 'react';

import { Tabs } from '@/components/Tabs';
import { problemOf } from '@/features/shared/problem';
import { pvp as data } from '@/gamedata/pvp';
import type { LeaderboardRow } from '@/net/rest';
import { className } from '@/progression/classes';
import { rankName } from '@/progression/names';
import { character } from '@/state/character';
import { pvpProfile, standingIn } from '@/state/pvp-profile';

import type { BoardReader } from './boards';
import { recordOf } from './modes';

const rated = data.modes.filter((m) => m.rated);

export function Leaderboard({ board }: { readonly board: BoardReader }) {
  const [mode, setMode] = useState<string>(rated[0]?.id ?? '');
  const [read, setRead] = useState<{
    readonly mode: string;
    readonly rows: readonly LeaderboardRow[];
  } | null>(null);
  const [failed, setFailed] = useState<{ readonly mode: string; readonly text: string } | null>(
    null,
  );
  const me = character.use((s) => s.character);
  const mine = pvpProfile.use((s) => standingIn(s.standings, mode));
  useEffect(() => {
    let live = true;
    board(mode).then(
      (b) => {
        if (live) {
          setRead({ mode, rows: b.rows });
        }
      },
      (err: unknown) => {
        if (live) {
          setFailed({ mode, text: problemOf(err) });
        }
      },
    );
    return () => {
      live = false;
    };
  }, [board, mode]);
  const rows = read?.mode === mode ? read.rows : null;
  const problem = failed?.mode === mode && rows === null ? failed.text : null;
  return (
    <div className="arena-board">
      <Tabs
        label="Mode"
        tabs={rated.map((m) => ({ id: m.id, name: m.name }))}
        chosen={mode}
        onChoose={setMode}
      />
      {problem ? (
        <p className="error" role="alert">
          {problem}
        </p>
      ) : rows === null ? (
        <p className="muted">Reading the board…</p>
      ) : rows.length === 0 ? (
        <p className="muted">No one has placed yet.</p>
      ) : (
        <div className="arena-board-scroll">
          <table aria-label="Leaderboard">
            <thead>
              <tr>
                <th scope="col">#</th>
                <th scope="col">Name</th>
                <th scope="col">Rating</th>
                <th scope="col" title="Wins, losses and draws">
                  W–L–D
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.character_id} row={r} mine={r.character_id === me?.id} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {mine && rows !== null && (
        <p className="arena-board-own" aria-label="Your place">
          {mine.placement_left > 0
            ? `You’re placing: ${data.placement - mine.placement_left} of ${data.placement} played.`
            : `You: #${mine.place}, ${mine.shown} ${rankName(mine.rank)}, ${recordOf(mine)}.`}
        </p>
      )}
    </div>
  );
}

function Row({ row, mine }: { readonly row: LeaderboardRow; readonly mine: boolean }) {
  return (
    <tr className={mine ? 'arena-board-mine' : undefined} aria-current={mine || undefined}>
      <td>{row.place}</td>
      <td>
        {row.name}
        <span className="arena-board-sub">{className(row.class)}</span>
      </td>
      <td>
        {row.shown}
        <span className="arena-board-sub">{rankName(row.rank)}</span>
      </td>
      <td className="muted">
        {row.wins}–{row.losses}–{row.draws}
      </td>
    </tr>
  );
}
