// A draft is tied to the unspent count it began from: any change to that count voids it, so an Apply resent after it landed never spends twice.
import { useState } from 'react';

import { RequestError } from '@/net/socket';
import type { ErrorCode } from '@/protocol/errors';

export interface Staged<T> {
  /** Staged, not yet applied; empty once the unspent count moves. */
  readonly staged: T;
  readonly stage: (next: T) => void;
  readonly undo: () => void;
  /** A request is on its way. */
  readonly busy: boolean;
  readonly notice: string | null;
  /** Sends a spend; the draft clears once it lands, or once changed says the count moved under it. */
  readonly send: (request: () => Promise<unknown>) => void;
}

export function useStaged<T>(unspent: number, empty: T, changed: ErrorCode): Staged<T> {
  const [draft, setDraft] = useState<{ base: number; staged: T }>({ base: unspent, staged: empty });
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const undo = () => setDraft({ base: unspent, staged: empty });

  const send = async (request: () => Promise<unknown>) => {
    setBusy(true);
    setNotice(null);
    try {
      await request();
      undo();
    } catch (err) {
      if (err instanceof RequestError && err.code === changed) {
        // The count moved: a resent request that already landed, whose StatsChanged is here.
        undo();
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

  return {
    staged: draft.base === unspent ? draft.staged : empty,
    stage: (next) => setDraft({ base: unspent, staged: next }),
    undo,
    busy,
    notice,
    send: (request) => {
      if (!busy) {
        void send(request);
      }
    },
  };
}
