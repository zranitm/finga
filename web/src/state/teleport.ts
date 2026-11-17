// The player's teleport channel as the screen shows it (teleport.md section 4): from the Response that started it till its MapChange, its TeleportCancelled, or a key the server cancels it for.
import { defineStore } from './store';

export interface TeleportState {
  readonly to: string | null; // the map it takes the player to
  readonly started: number; // counts the channels, so each new one draws afresh
  start(to: string): void;
  end(): void;
}

export const teleport = defineStore<TeleportState>((set, get) => ({
  to: null,
  started: 0,
  start: (to) => {
    set({ to, started: get().started + 1 });
  },
  end: () => {
    if (get().to !== null) {
      set({ to: null });
    }
  },
}));
