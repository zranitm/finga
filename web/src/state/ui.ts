// Which windows are open and in what order, and whether the chat input is asked to take focus; what has the keyboard is read from the focus itself.
import { defineStore } from './store';

export type WindowName =
  'inventory' | 'character' | 'skills' | 'party' | 'quests' | 'npc' | 'settings';

export interface UiState {
  /** The open windows, the top-most last. */
  readonly windows: readonly WindowName[];
  /** Asks the chat input to take focus. */
  readonly chatOpen: boolean;
  /** Why the last item request was refused, and the window it came from: the next request anywhere makes it stale. */
  readonly itemNotice: { readonly from: WindowName; readonly text: string } | null;
  /** Shows text in from; null clears whatever notice is up. */
  setItemNotice(from: WindowName, text: string | null): void;
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
  itemNotice: null,
  setItemNotice: (from, text) => {
    set({ itemNotice: text === null ? null : { from, text } });
  },
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
    if (get().windows.includes(name)) {
      get().closeWindow(name);
    } else {
      set({ windows: [...get().windows, name] });
    }
  },
  openWindow: (name) => {
    const { windows } = get();
    if (windows.at(-1) !== name) {
      set({ windows: [...without(windows, name), name] });
    }
  },
  closeWindow: (name) => {
    const { windows, itemNotice } = get();
    if (windows.includes(name)) {
      // A window opened again starts without the notice it closed with.
      set({
        windows: without(windows, name),
        itemNotice: itemNotice?.from === name ? null : itemNotice,
      });
    }
  },
  closeTopWindow: () => {
    const top = get().windows.at(-1);
    if (top !== undefined) {
      get().closeWindow(top);
    }
  },
  clear: () => {
    set({ windows: [], chatOpen: false, itemNotice: null });
  },
}));
