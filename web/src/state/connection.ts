// Fed by app/wiring; state never imports net.
import type { KickReason } from '@/protocol/messages';

import { defineStore } from './store';

export type ConnectionStatus =
  'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'kicked';

export interface ConnectionState {
  readonly status: ConnectionStatus;
  readonly attempt: number;
  readonly kick: KickReason | null;
  readonly rtt: number | null;
  /** The socket is open but the world has sent no frame for a while; the scene holds the player still. */
  readonly worldStalled: boolean;
  setConnection(status: ConnectionStatus, extra?: { attempt?: number; kick?: KickReason }): void;
  setRtt(rtt: number | null): void;
  setWorldStalled(stalled: boolean): void;
}

export const connection = defineStore<ConnectionState>((set) => ({
  status: 'disconnected',
  attempt: 0,
  kick: null,
  rtt: null,
  worldStalled: false,
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
  setWorldStalled: (worldStalled) => {
    set({ worldStalled });
  },
}));
