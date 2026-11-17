// The locked mob, for the target window (target-lock.md section 5): the world scene sets it as the lock or its HP changes; display only.
import { defineStore } from './store';

export interface Target {
  readonly mobId: number;
  readonly type: string;
  readonly hp: number;
}

export interface TargetState {
  readonly target: Target | null;
  /** The locked mob as drawn now, or null with none; the same again changes nothing. */
  show(target: Target | null): void;
}

export const target = defineStore<TargetState>((set, get) => ({
  target: null,
  show: (t) => {
    const now = get().target;
    if (now?.mobId !== t?.mobId || now?.type !== t?.type || now?.hp !== t?.hp) {
      set({ target: t });
    }
  },
}));
