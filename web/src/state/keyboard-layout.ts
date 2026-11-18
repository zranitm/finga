// What the player's keyboard prints on each key, by KeyboardEvent.code, where the browser tells it (Chrome and Edge); elsewhere none, and keys go by their US names.
import { defineStore } from './store';

export interface KeyboardLayoutState {
  readonly printed: ReadonlyMap<string, string> | undefined;
  setPrinted(printed: ReadonlyMap<string, string>): void;
}

export const keyboardLayout = defineStore<KeyboardLayoutState>((set) => ({
  printed: undefined,
  setPrinted: (printed) => {
    set({ printed });
  },
}));
