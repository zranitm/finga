// Which windows are open and in what order, and whether the chat input is asked to take focus; what has the keyboard is read from the focus itself.
import { defineStore } from './store';

export type WindowName = 'inventory' | 'character' | 'party' | 'settings';

export interface UiState {
  /** The open windows, the top-most last. */
  readonly windows: readonly WindowName[];
  /** Asks the chat input to take focus. */
  readonly chatOpen: boolean;
  /** Closes an open window, else opens it on top. */
  toggleWindow(name: WindowName): void;
  openChat(): void;
  closeChat(): void;
  /** Opens a window on top, or brings an open one there. */
  openWindow(name: WindowName): void;
  closeWindow(name: WindowName): void;
  /** Escape's: the top-most window, whatever has focus. */
  closeTopWindow(): void;
  /** Leaving the world: no window or chat stays asked for. */
  clear(): void;
}

const without = (windows: readonly WindowName[], name: WindowName) =>
  windows.filter((w) => w !== name);

export const ui = defineStore<UiState>((set, get) => ({
  windows: [],
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
    const { windows } = get();
    set({ windows: windows.includes(name) ? without(windows, name) : [...windows, name] });
  },
  openWindow: (name) => {
    const { windows } = get();
    if (windows.at(-1) !== name) {
      set({ windows: [...without(windows, name), name] });
    }
  },
  closeWindow: (name) => {
    const { windows } = get();
    if (windows.includes(name)) {
      set({ windows: without(windows, name) });
    }
  },
  closeTopWindow: () => {
    const top = get().windows.at(-1);
    if (top !== undefined) {
      get().closeWindow(top);
    }
  },
  clear: () => {
    set({ windows: [], chatOpen: false });
  },
}));
