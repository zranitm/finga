// A window's item requests: pending until the Response, and after a timeout or a lost connection retried with the same idempotency key.
import { useState } from 'react';

import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';

export interface PendingRequest {
  readonly key: string;
  readonly send: () => Promise<unknown>;
  readonly retryable: boolean;
  readonly error: string | null;
}

export interface ItemRequests {
  /** By what each request acts on: an item ID, or an equip slot. */
  readonly pending: ReadonlyMap<string, PendingRequest>;
  /** Why the last request was refused, until the next one. */
  readonly notice: string | null;
  readonly setNotice: (notice: string | null) => void;
  /** Sends request with a new idempotency key; InventoryChanged, not the Response, moves the items. */
  readonly run: (target: string, request: (key: string) => Promise<unknown>) => void;
  readonly retry: (target: string) => void;
}

export function useItemRequests(): ItemRequests {
  const [pending, setPending] = useState<ReadonlyMap<string, PendingRequest>>(new Map());
  const [notice, setNotice] = useState<string | null>(null);

  const setPendingFor = (target: string, p: PendingRequest | null) => {
    setPending((prev) => {
      const next = new Map(prev);
      if (p) {
        next.set(target, p);
      } else {
        next.delete(target);
      }
      return next;
    });
  };

  const send = async (target: string, key: string, request: () => Promise<unknown>) => {
    setPendingFor(target, { key, send: request, retryable: false, error: null });
    setNotice(null);
    try {
      await request();
      setPendingFor(target, null);
    } catch (err) {
      if (err instanceof RequestError && (err.code === 'timeout' || err.code === 'disconnected')) {
        setPendingFor(target, { key, send: request, retryable: true, error: 'No answer yet.' });
      } else {
        setPendingFor(target, null);
        setNotice(err instanceof Error ? err.message : 'The server refused.');
      }
    }
  };

  return {
    pending,
    notice,
    setNotice,
    run: (target, request) => {
      const key = idempotencyKey();
      void send(target, key, () => request(key));
    },
    retry: (target) => {
      const p = pending.get(target);
      if (p?.retryable) {
        void send(target, p.key, p.send);
      }
    },
  };
}
