// While captureKeyboard is set, the game ignores movement keys.
import { defineStore } from './store';

export type WindowName = 'inventory' | 'party' | 'settings';

export interface UiState {
  readonly windows: Readonly<Record<WindowName, boolean>>;
  readonly captureKeyboard: boolean;
  /** Asks the chat input to take focus. */
  readonly chatOpen: boolean;
  toggleWindow(name: WindowName): void;
  openChat(): void;
  closeChat(): void;
  openWindow(name: WindowName): void;
  closeWindow(name: WindowName): void;
  setCaptureKeyboard(capture: boolean): void;
}

const closed: Record<WindowName, boolean> = { inventory: false, party: false, settings: false };

export const ui = defineStore<UiState>((set, get) => ({
  windows: closed,
  captureKeyboard: false,
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
  setCaptureKeyboard: (capture) => {
    if (get().captureKeyboard !== capture) {
      set({ captureKeyboard: capture });
    }
  },
}));
