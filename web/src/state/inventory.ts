// Only Welcome and InventoryChanged change it, so a refused move leaves it as it was.
import type { Data, ItemView } from '@/protocol/messages';

import { defineStore } from './store';

export const SLOTS_PER_TAB = 24;
export const tabs = ['equip', 'use', 'etc'] as const;
export type Tab = (typeof tabs)[number];

/** Tabs are strings on the wire; a slot in a tab this client does not know is skipped. */
export function isTab(tab: string): tab is Tab {
  return (tabs as readonly string[]).includes(tab);
}

export type Slots = Readonly<Record<Tab, readonly (ItemView | null)[]>>;

export interface InventoryState {
  readonly slots: Slots;
  applyWelcome(d: Data<'welcome'>): void;
  applyInventoryChanged(d: Data<'inventoryChanged'>): void;
  clear(): void;
}

function emptySlots(): Slots {
  return {
    equip: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    use: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    etc: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
  };
}

function place(slots: Slots, tab: string, slot: number, item: ItemView | null): Slots {
  if (!isTab(tab) || slot < 0 || slot >= SLOTS_PER_TAB) {
    return slots;
  }
  const row = [...slots[tab]];
  row[slot] = item;
  return { ...slots, [tab]: row };
}

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
