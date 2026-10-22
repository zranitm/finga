// The inventory as the server last described it: three tabs of 24 slots
// (page 5). The store never rules; it places what Welcome and
// InventoryChanged say, so a refused move, which arrives as a Response
// with nothing else, leaves it unchanged.
import type { Data, ItemView, Tab } from '@/protocol/messages';

import { defineStore } from './store';

export const SLOTS_PER_TAB = 24;
export const tabs: readonly Tab[] = ['equip', 'use', 'etc'];

export type Slots = Readonly<Record<Tab, readonly (ItemView | null)[]>>;

export interface InventoryState {
  readonly slots: Slots;
  applyWelcome(d: Data<'Welcome'>): void;
  applyInventoryChanged(d: Data<'InventoryChanged'>): void;
  clear(): void;
}

function emptySlots(): Slots {
  return {
    equip: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    use: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    etc: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
  };
}

function place(slots: Slots, tab: Tab, slot: number, item: ItemView | null): Slots {
  if (slot < 0 || slot >= SLOTS_PER_TAB) {
    return slots; // not a slot this inventory has; the server would not send it
  }
  const row = [...slots[tab]];
  row[slot] = item;
  return { ...slots, [tab]: row };
}

/** Every item in the inventory, tab by tab, slot order. */
export function allItems(slots: Slots): ItemView[] {
  return tabs.flatMap((tab) => slots[tab].filter((it): it is ItemView => it !== null));
}

export const inventory = defineStore<InventoryState>((set, get) => ({
  slots: emptySlots(),
  applyWelcome: (d) => {
    let slots = emptySlots();
    for (const item of d.inventory) {
      slots = place(slots, item.tab, item.slot, item);
    }
    set({ slots });
  },
  applyInventoryChanged: (d) => {
    let slots = get().slots;
    for (const s of d.slots) {
      slots = place(slots, s.tab, s.slot, s.item ?? null);
    }
    set({ slots });
  },
  clear: () => {
    set({ slots: emptySlots() });
  },
}));
