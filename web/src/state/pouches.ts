// The player's own pouches and boss boxes on their map, which nobody else is sent: the Snapshot's, then each Delta's changes; which are within reach, as the world scene finds each frame; and the picks asked for, until answered or refused.
import { errorText } from '@/protocol/errors';
import type { Data, PouchItem, PouchKind } from '@/protocol/messages';

import { defineStore } from './store';

type PouchWire = Data<'snapshot'>['pouches'][number];

export interface Pouch {
  readonly id: number;
  readonly kind: PouchKind;
  readonly x: number;
  readonly y: number;
  readonly z: number;
  readonly items: readonly PouchItem[];
  /** When its life ends, performance.now() milliseconds. */
  readonly goneAt: number;
}

/** An item a take asks for: its pouch and its id in it. */
export interface ItemPick {
  readonly pouch: number;
  readonly item: number;
}

export const pickKey = (p: ItemPick): string => `${p.pouch}/${p.item}`;

/** Every item in the pouches within reach, as Take all asks for them. */
export function picksInReach(s: Pick<PouchesState, 'pouches' | 'inReach'>): ItemPick[] {
  return s.pouches
    .filter((p) => s.inReach.includes(p.id))
    .flatMap((p) => p.items.map((it) => ({ pouch: p.id, item: it.item })));
}

/** The answer a take's request gave, or the error it failed with. */
export type TakeRequest = (picks: readonly ItemPick[]) => Promise<Data<'response'>>;

export interface PouchesState {
  /** By id, oldest first. */
  readonly pouches: readonly Pouch[];
  /** The pouches within reach of the player, by id. */
  readonly inReach: readonly number[];
  /** Picks asked for whose items are still in their pouches. */
  readonly taking: ReadonlySet<string>;
  /** Picks refused, and why, until they're asked for again or their items go. */
  readonly refused: ReadonlyMap<string, string>;
  applySnapshot(pouches: readonly PouchWire[]): void;
  applyDelta(changed: readonly PouchWire[], gone: readonly number[]): void;
  setInReach(ids: readonly number[]): void;
  /** Asks for picks not already being taken, through request; a refused pick keeps its reason, and a taken one waits for its item to leave its pouch. */
  take(picks: readonly ItemPick[], request: TakeRequest): Promise<void>;
  clear(): void;
}

const pouchOf = (w: PouchWire, now: number): Pouch => ({
  id: w.id,
  kind: w.kind,
  x: w.x,
  y: w.y,
  z: w.z,
  items: w.items,
  goneAt: now + Number(w.goneInMs),
});

/** The keys of every item still in pouches. */
const held = (pouches: readonly Pouch[]) =>
  new Set(pouches.flatMap((p) => p.items.map((it) => pickKey({ pouch: p.id, item: it.item }))));

export const pouches = defineStore<PouchesState>((set, get) => {
  /** The pouches now, the picks whose items left them forgotten. */
  const put = (next: readonly Pouch[]) => {
    const keys = held(next);
    const { taking, refused } = get();
    set({
      pouches: next,
      taking: new Set([...taking].filter((k) => keys.has(k))),
      refused: new Map([...refused].filter(([k]) => keys.has(k))),
    });
  };
  return {
    pouches: [],
    inReach: [],
    taking: new Set(),
    refused: new Map(),
    applySnapshot: (wire) => {
      const now = performance.now();
      put(wire.map((w) => pouchOf(w, now)).sort((a, b) => a.id - b.id));
    },
    applyDelta: (changed, gone) => {
      if (changed.length === 0 && gone.length === 0) {
        return;
      }
      const now = performance.now();
      const byId = new Map(get().pouches.map((p) => [p.id, p]));
      for (const w of changed) {
        byId.set(w.id, pouchOf(w, now));
      }
      for (const id of gone) {
        byId.delete(id);
      }
      put([...byId.values()].sort((a, b) => a.id - b.id));
    },
    setInReach: (ids) => {
      const now = get().inReach;
      if (ids.length !== now.length || ids.some((id, i) => id !== now[i])) {
        set({ inReach: ids });
      }
    },
    take: async (picks, request) => {
      const asked = picks.filter((p) => !get().taking.has(pickKey(p)));
      if (asked.length === 0) {
        return;
      }
      const keys = asked.map(pickKey);
      const refused = new Map(get().refused);
      keys.forEach((k) => refused.delete(k));
      set({ taking: new Set([...get().taking, ...keys]), refused });
      let why: ReadonlyMap<string, string>;
      try {
        const answer = await request(asked);
        why = new Map(
          answer.picks.flatMap((a) =>
            a.pick && a.code !== 0 ? [[pickKey(a.pick), errorText(a.code)] as const] : [],
          ),
        );
      } catch (err) {
        const text = err instanceof Error ? err.message : String(err);
        why = new Map(keys.map((k) => [k, text]));
      }
      const live = held(get().pouches);
      const taking = new Set(get().taking);
      const refusedNow = new Map(get().refused);
      for (const [k, text] of why) {
        taking.delete(k);
        if (live.has(k)) {
          refusedNow.set(k, text);
        }
      }
      set({ taking, refused: refusedNow });
    },
    clear: () => {
      set({ pouches: [], inReach: [], taking: new Set(), refused: new Map() });
    },
  };
});
