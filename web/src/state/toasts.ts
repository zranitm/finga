import { defineStore } from './store';

export type ToastKind = 'error' | 'info';

export interface Toast {
  readonly id: number;
  readonly text: string;
  readonly kind: ToastKind;
}

export interface ToastsState {
  readonly toasts: readonly Toast[];
  /** Shows text, or gives the id of the same text of the same kind already showing. */
  push(text: string, kind?: ToastKind): number;
  dismiss(id: number): void;
  clear(): void;
}

/** The oldest goes first. */
export const MAX_TOASTS = 4;

let nextId = 1;

export const toasts = defineStore<ToastsState>((set, get) => ({
  toasts: [],
  push: (text, kind = 'error') => {
    const shown = get().toasts.find((t) => t.text === text && t.kind === kind);
    if (shown) {
      return shown.id; // the same words twice at once say no more than once
    }
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
