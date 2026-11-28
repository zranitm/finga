// The player's teleport channel as the screen shows it (teleport.md section 4): from the Response that started it till its MapChange, its TeleportCancelled, or a key the server cancels it for. A switch of channels runs on it too (large-regions.md section 4).
import { defineStore } from './store';

/** Where a channel takes the player: a waypoint, named as the bar shows it, or another channel of their region. */
export type Destination =
  { readonly waypoint: string; readonly name: string } | { readonly channel: number };

export interface TeleportState {
  readonly to: Destination | null;
  readonly last: Destination | null; // the latest started, kept past its end so the server's word of a cancel names it
  readonly started: number; // counts the channels, so each new one draws afresh
  start(to: Destination): void;
  end(): void;
}

export const teleport = defineStore<TeleportState>((set, get) => ({
  to: null,
  last: null,
  started: 0,
  start: (to) => {
    set({ to, last: to, started: get().started + 1 });
  },
  end: () => {
    if (get().to !== null) {
      set({ to: null });
    }
  },
}));
