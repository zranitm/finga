// What the player should know about the connection: connecting, connected,
// reconnecting (with the attempt), or kicked (with the reason). app/wiring
// feeds it from the socket's status events; state never imports net.
import type { KickReason } from '@/protocol/errors';

import { defineStore } from './store';

export type ConnectionStatus =
  'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'kicked';

export interface ConnectionState {
  readonly status: ConnectionStatus;
  readonly attempt: number; // reconnect attempts so far
  readonly kick: KickReason | null; // when status is kicked
  readonly rtt: number | null; // milliseconds, from the last Pong
  setConnection(status: ConnectionStatus, extra?: { attempt?: number; kick?: KickReason }): void;
  setRtt(rtt: number | null): void;
}

export const connection = defineStore<ConnectionState>((set) => ({
  status: 'disconnected',
  attempt: 0,
  kick: null,
  rtt: null,
  setConnection: (status, extra = {}) => {
    set({
      status,
      attempt: extra.attempt ?? 0,
      kick: status === 'kicked' ? (extra.kick ?? null) : null,
    });
  },
  setRtt: (rtt) => {
    set({ rtt });
  },
}));
