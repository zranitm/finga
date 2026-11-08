// Which windows are open, and whether the chat input is asked to take focus; what has the keyboard is read from the focus itself.
import { defineStore } from './store';

export type WindowName = 'inventory' | 'character' | 'party' | 'settings';

export interface UiState {
  readonly windows: Readonly<Record<WindowName, boolean>>;
  /** Asks the chat input to take focus. */
  readonly chatOpen: boolean;
  toggleWindow(name: WindowName): void;
  openChat(): void;
  closeChat(): void;
  openWindow(name: WindowName): void;
  closeWindow(name: WindowName): void;
}

const closed: Record<WindowName, boolean> = {
  inventory: false,
  character: false,
  party: false,
  settings: false,
};

export const ui = defineStore<UiState>((set, get) => ({
  windows: closed,
  chatOpen: false,
  openChat: () => {
    if (!get().chatOpen) {
      set({ chatOpen: true });
    }
  },
  closeChat: () => {
    if (get().chatOpen) {
      set({ chatOpen: false });
    }
  },
  toggleWindow: (name) => {
    set({ windows: { ...get().windows, [name]: !get().windows[name] } });
  },
  openWindow: (name) => {
    set({ windows: { ...get().windows, [name]: true } });
  },
  closeWindow: (name) => {
    set({ windows: { ...get().windows, [name]: false } });
  },
}));
