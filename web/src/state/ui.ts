// Which windows are open, and whether a text field or window has the
// keyboard: while captureKeyboard is set, the game ignores movement keys
// (page 7, Phaser and React together).
import { defineStore } from './store';

export type WindowName = 'inventory' | 'party' | 'settings';

export interface UiState {
  readonly windows: Readonly<Record<WindowName, boolean>>;
  readonly captureKeyboard: boolean;
  /** The chat input is asked to take focus (the Enter key, C13). */
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
