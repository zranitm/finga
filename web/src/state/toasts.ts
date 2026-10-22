// Short notices over the game (C11): a refused pickup, a busy server.
// Pushed by app/wiring from Responses with no request ID, shown by the HUD,
// dismissed by a click or after a few seconds.
import { defineStore } from './store';

export type ToastKind = 'error' | 'info';

export interface Toast {
  readonly id: number;
  readonly text: string;
  readonly kind: ToastKind;
}

export interface ToastsState {
  readonly toasts: readonly Toast[];
  push(text: string, kind?: ToastKind): number;
  dismiss(id: number): void;
  clear(): void;
}

/** Never more than this many at once; the oldest goes first. */
export const MAX_TOASTS = 4;

let nextId = 1;

export const toasts = defineStore<ToastsState>((set, get) => ({
  toasts: [],
  push: (text, kind = 'error') => {
    const id = nextId++;
    const list = [...get().toasts, { id, text, kind }];
    set({ toasts: list.slice(-MAX_TOASTS) });
    return id;
  },
  dismiss: (id) => {
    set({ toasts: get().toasts.filter((t) => t.id !== id) });
  },
  clear: () => {
    set({ toasts: [] });
  },
}));
