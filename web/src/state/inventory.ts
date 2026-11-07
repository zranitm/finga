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

type Tabs<T> = Readonly<Record<Tab, readonly T[]>>;

export type Slots = Tabs<ItemView | null>;

/** The inventory revision that last set each slot. */
type Revisions = Tabs<bigint>;

export interface InventoryState {
  readonly slots: Slots;
  readonly revisions: Revisions;
  applyWelcome(d: Data<'welcome'>): void;
  /** A slot takes a change at least as new as its own revision; revision 0, stored before revisions, applies as it comes. */
  applyInventoryChanged(d: Data<'inventoryChanged'>): void;
  clear(): void;
}

function filled<T>(value: T): Tabs<T> {
  return {
    equip: Array<T>(SLOTS_PER_TAB).fill(value),
    use: Array<T>(SLOTS_PER_TAB).fill(value),
    etc: Array<T>(SLOTS_PER_TAB).fill(value),
  };
}

function inRange(tab: string, slot: number): tab is Tab {
  return isTab(tab) && slot >= 0 && slot < SLOTS_PER_TAB;
}

function put<T>(tabs: Tabs<T>, tab: Tab, slot: number, value: T): Tabs<T> {
  const row = [...tabs[tab]];
  row[slot] = value;
  return { ...tabs, [tab]: row };
}

export function allItems(slots: Slots): ItemView[] {
  return tabs.flatMap((tab) => slots[tab].filter((it): it is ItemView => it !== null));
}

export const inventory = defineStore<InventoryState>((set, get) => ({
  slots: filled<ItemView | null>(null),
  revisions: filled(0n),
  applyWelcome: (d) => {
    let slots = filled<ItemView | null>(null);
    for (const item of d.inventory) {
      if (inRange(item.tab, item.slot)) {
        slots = put(slots, item.tab, item.slot, item);
      }
    }
    set({ slots, revisions: filled(d.inventoryRevision) });
  },
  applyInventoryChanged: (d) => {
    let { slots, revisions } = get();
    for (const s of d.slots) {
      if (!inRange(s.tab, s.slot)) {
        continue;
      }
      if (d.revision === 0n) {
        slots = put(slots, s.tab, s.slot, s.item ?? null);
      } else if (d.revision >= (revisions[s.tab][s.slot] ?? 0n)) {
        slots = put(slots, s.tab, s.slot, s.item ?? null);
        revisions = put(revisions, s.tab, s.slot, d.revision);
      }
    }
    set({ slots, revisions });
  },
  clear: () => {
    set({ slots: filled<ItemView | null>(null), revisions: filled(0n) });
  },
}));
