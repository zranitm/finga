// A draft is tied to the unspent count it began from: any change to that count voids it, so an Apply resent after it landed never spends twice.
import { useState } from 'react';

import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import { addStats, autoAssign, hasSplit, noPoints, total } from '@/progression/stats';
import type { Primary, PrimaryKey } from '@/progression/stats';
import { ERR } from '@/protocol/errors';
import { character } from '@/state/character';

export interface Points {
  readonly unspent: number;
  /** Staged, not yet applied. */
  readonly staged: Primary;
  /** Unspent less staged. */
  readonly left: number;
  readonly spent: number;
  readonly freeResetLeft: boolean;
  /** A request is on its way. */
  readonly busy: boolean;
  readonly notice: string | null;
  readonly stage: (k: PrimaryKey) => void;
  readonly undo: () => void;
  readonly apply: () => void;
  readonly autoAssign: () => void;
  /** The class has a split to auto-assign by; a Beginner has none. */
  readonly canAutoAssign: boolean;
  readonly reset: () => void;
}

export function usePoints(socket: Pick<GameSocket, 'request'>): Points {
  const c = character.use((s) => s.character);
  const p = c?.sheet?.points;
  const unspent = p?.unspent ?? 0;
  const [draft, setDraft] = useState<{ base: number; points: Primary }>({
    base: unspent,
    points: noPoints,
  });
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const staged = draft.base === unspent ? draft.points : noPoints;
  const left = unspent - total(staged);

  const send = async (request: () => Promise<unknown>) => {
    setBusy(true);
    setNotice(null);
    try {
      await request();
      setDraft({ base: unspent, points: noPoints });
    } catch (err) {
      if (err instanceof RequestError && err.code === ERR.POINTS_CHANGED) {
        // The count moved: a resent request that already landed, whose StatsChanged is here.
        setDraft({ base: unspent, points: noPoints });
      } else if (
        err instanceof RequestError &&
        (err.code === 'timeout' || err.code === 'disconnected')
      ) {
        setNotice('No answer yet. Try again.');
      } else {
        setNotice(err instanceof Error ? err.message : 'The server refused.');
      }
    } finally {
      setBusy(false);
    }
  };

  const spend = (points: Primary) => {
    if (!busy && total(points) > 0) {
      void send(() => socket.request('spendPoints', { expectedUnspent: unspent, points }));
    }
  };

  return {
    unspent,
    staged,
    left,
    spent: total(p?.spent ?? noPoints),
    freeResetLeft: p?.freeResetLeft ?? false,
    busy,
    notice,
    stage: (k) => {
      if (left > 0) {
        setDraft({ base: unspent, points: addStats(staged, { [k]: 1 }) });
      }
    },
    undo: () => setDraft({ base: unspent, points: noPoints }),
    apply: () => spend(staged),
    autoAssign: () => spend(autoAssign(c?.class ?? '', unspent)),
    canAutoAssign: hasSplit(c?.class ?? ''),
    reset: () => {
      if (!busy) {
        void send(() => socket.request('resetPoints', { expectedUnspent: unspent }));
      }
    },
  };
}
