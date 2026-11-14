// The energy the player sees: the world's count as of its last Delta's ack, less each of the player's own skill presses during or past that ack's input, so a spend shows the moment it's pressed. The world counts a press from the input after the one it came during, since it may take the press in after that input.
import { defineStore } from './store';

interface Spend {
  readonly seq: number;
  readonly cost: number;
}

export interface EnergyState {
  /** The world's count, as of ackSeq. */
  readonly told: number;
  readonly ackSeq: number;
  readonly pending: readonly Spend[];
  /** told less the pending spends: what the pips and the skill keys read. */
  readonly shown: number;
  /** A Welcome's count, with no press pending. */
  welcome(energy: number): void;
  /** A Delta: its ack covers the presses before it, and its energy, when set, is the world's new count. */
  delta(ackSeq: number, energy: number | undefined): void;
  /** A skill pressed during input seq; the returned function takes the press back, for a refusal. */
  spend(seq: number, cost: number): () => void;
  /** A new map's join: its frames count every press made before it. */
  joined(): void;
  clear(): void;
}

const shownOf = (told: number, pending: readonly Spend[]) =>
  Math.max(
    0,
    pending.reduce((n, s) => n - s.cost, told),
  );

export const energy = defineStore<EnergyState>((set, get) => ({
  told: 0,
  ackSeq: 0,
  pending: [],
  shown: 0,
  welcome: (n) => {
    set({ told: n, ackSeq: 0, pending: [], shown: n });
  },
  delta: (ackSeq, n) => {
    const s = get();
    const told = n ?? s.told;
    const pending = s.pending.filter((p) => p.seq >= ackSeq);
    if (told !== s.told || pending.length !== s.pending.length || ackSeq !== s.ackSeq) {
      set({ told, ackSeq, pending, shown: shownOf(told, pending) });
    }
  },
  spend: (seq, cost) => {
    const press = { seq, cost };
    const s = get();
    const pending = [...s.pending, press];
    set({ pending, shown: shownOf(s.told, pending) });
    return () => {
      const now = get();
      if (now.pending.includes(press)) {
        const rest = now.pending.filter((p) => p !== press);
        set({ pending: rest, shown: shownOf(now.told, rest) });
      }
    };
  },
  joined: () => {
    const s = get();
    if (s.pending.length > 0) {
      set({ pending: [], shown: s.told });
    }
  },
  clear: () => {
    set({ told: 0, ackSeq: 0, pending: [], shown: 0 });
  },
}));
