// Only Welcome and InventoryChanged change it, so a refused move leaves it as it was.
import { equipSlots } from '@/gamedata/equip-slots';
import type { Data, ItemView } from '@/protocol/messages';

import { defineStore } from './store';

export const SLOTS_PER_TAB = 24;
export const tabs = ['equip', 'use', 'etc'] as const;
export type Tab = (typeof tabs)[number];

/** Tabs are strings on the wire; a slot in a tab this client does not know is skipped. */
export function isTab(tab: string): tab is Tab {
  return (tabs as readonly string[]).includes(tab);
}

export type EquipSlotId = (typeof equipSlots)[number]['id'];
/** The ten equip slots, in the character window's order. */
export const equipSlotIds: readonly EquipSlotId[] = equipSlots.map((s) => s.id);

/** Equip slots are strings on the wire; one this client does not know is skipped. */
export function isEquipSlot(id: string): id is EquipSlotId {
  return (equipSlotIds as readonly string[]).includes(id);
}

type Tabs<T> = Readonly<Record<Tab, readonly T[]>>;
type PerEquipSlot<T> = Readonly<Record<EquipSlotId, T>>;

export type Slots = Tabs<ItemView | null>;
/** What each equip slot holds. */
export type Equipment = PerEquipSlot<ItemView | null>;

/** The inventory revision that last set each slot. */
type Revisions = Tabs<bigint>;

export interface InventoryState {
  readonly slots: Slots;
  readonly revisions: Revisions;
  readonly equipment: Equipment;
  /** Equip slots share the inventory's revisions and its rule. */
  readonly equipmentRevisions: PerEquipSlot<bigint>;
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

function each<T>(value: T): PerEquipSlot<T> {
  return Object.fromEntries(equipSlotIds.map((id) => [id, value])) as PerEquipSlot<T>;
}

/** Whether a change at revision lands on a slot last set at current: a newer one does, and 0, from before revisions, always. */
function lands(revision: bigint, current: bigint): boolean {
  return revision === 0n || revision >= current;
}

function inRange(tab: string, slot: number): tab is Tab {
  return isTab(tab) && slot >= 0 && slot < SLOTS_PER_TAB;
}

function put<T>(tabs: Tabs<T>, tab: Tab, slot: number, value: T): Tabs<T> {
  const row = [...tabs[tab]];
  row[slot] = value;
  return { ...tabs, [tab]: row };
}

export const inventory = defineStore<InventoryState>((set, get) => ({
  slots: filled<ItemView | null>(null),
  revisions: filled(0n),
  equipment: each<ItemView | null>(null),
  equipmentRevisions: each(0n),
  applyWelcome: (d) => {
    let slots = filled<ItemView | null>(null);
    for (const item of d.inventory) {
      if (inRange(item.tab, item.slot)) {
        slots = put(slots, item.tab, item.slot, item);
      }
    }
    const equipment: Record<EquipSlotId, ItemView | null> = { ...each<ItemView | null>(null) };
    for (const e of d.equipment) {
      if (isEquipSlot(e.equipSlot)) {
        equipment[e.equipSlot] = e.item ?? null;
      }
    }
    set({
      slots,
      revisions: filled(d.inventoryRevision),
      equipment,
      equipmentRevisions: each(d.inventoryRevision),
    });
  },
  applyInventoryChanged: (d) => {
    let { slots, revisions } = get();
    for (const s of d.slots) {
      if (!inRange(s.tab, s.slot) || !lands(d.revision, revisions[s.tab][s.slot] ?? 0n)) {
        continue;
      }
      slots = put(slots, s.tab, s.slot, s.item ?? null);
      if (d.revision !== 0n) {
        revisions = put(revisions, s.tab, s.slot, d.revision);
      }
    }
    let { equipment, equipmentRevisions } = get();
    for (const e of d.equipment) {
      if (!isEquipSlot(e.equipSlot) || !lands(d.revision, equipmentRevisions[e.equipSlot])) {
        continue;
      }
      equipment = { ...equipment, [e.equipSlot]: e.item ?? null };
      if (d.revision !== 0n) {
        equipmentRevisions = { ...equipmentRevisions, [e.equipSlot]: d.revision };
      }
    }
    set({ slots, revisions, equipment, equipmentRevisions });
  },
  clear: () => {
    set({
      slots: filled<ItemView | null>(null),
      revisions: filled(0n),
      equipment: each<ItemView | null>(null),
      equipmentRevisions: each(0n),
    });
  },
}));

/** How many of an item the tabs hold; worn items aren't counted, as a hand-in never takes them. */
export function heldOf(slots: Slots, template: string): number {
  let n = 0;
  for (const tab of tabs) {
    for (const item of slots[tab]) {
      if (item?.template === template) {
        n += item.quantity;
      }
    }
  }
  return n;
}
