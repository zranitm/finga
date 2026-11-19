// The lock, for the target window (target-lock.md section 5, pvp.md section 13): a mob, or in a match an enemy player; the world scene sets it as the lock or its HP changes; display only.
import { defineStore } from './store';

/** A locked mob: its kind and HP as drawn. */
export interface MobTarget {
  readonly kind: 'mob';
  readonly ref: string;
  readonly type: string;
  readonly hp: number;
}

/** A locked enemy player, or a bot: who they are and their HP, as of the newest frame. */
export interface PlayerTarget {
  readonly kind: 'player';
  readonly ref: string;
  readonly name: string;
  readonly cls: string;
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  readonly bot: boolean;
}

export type Target = MobTarget | PlayerTarget;

export interface TargetState {
  readonly target: Target | null;
  /** The lock as drawn now, or null with none; the same again changes nothing. */
  show(target: Target | null): void;
}

/** Whether two targets show the same. */
function same(a: Target | null, b: Target | null): boolean {
  if (a === null || b === null) {
    return a === b;
  }
  const keys = Object.keys(a) as (keyof Target)[];
  return keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k]);
}

export const target = defineStore<TargetState>((set, get) => ({
  target: null,
  show: (t) => {
    if (!same(get().target, t)) {
      set({ target: t });
    }
  },
}));
