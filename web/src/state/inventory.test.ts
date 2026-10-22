import { beforeEach, describe, expect, it } from 'vitest';

import type { ItemView } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { allItems, inventory, SLOTS_PER_TAB } from './inventory';

const potion: ItemView = {
  item_id: 'i-potion',
  template: 'red_potion',
  tab: 'use',
  slot: 0,
  quantity: 5,
};
const sword: ItemView = {
  item_id: 'i-sword',
  template: 'wooden_sword',
  tab: 'equip',
  slot: 0,
  quantity: 1,
};
const gel: ItemView = { item_id: 'i-gel', template: 'slime_gel', tab: 'etc', slot: 2, quantity: 2 };

function welcomeWith(items: ItemView[]) {
  return {
    token: 't',
    character: {
      id: 'c',
      name: 'Ayla',
      class: 'warrior',
      level: 1,
      exp: 0,
      stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
      hp: 204,
      max_hp: 204,
      mp: 28,
      max_mp: 28,
    },
    inventory: items,
    map_id: 'town',
    tick: 1,
    server_time: 0,
  };
}

describe('inventory store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('starts with three empty tabs of 24 slots', () => {
    const { slots } = inventory.getState();
    for (const tab of ['equip', 'use', 'etc'] as const) {
      expect(slots[tab]).toHaveLength(SLOTS_PER_TAB);
      expect(slots[tab].every((s) => s === null)).toBe(true);
    }
  });

  it('places the Welcome inventory by tab and slot', () => {
    inventory.getState().applyWelcome(welcomeWith([potion, sword, gel]));
    const { slots } = inventory.getState();
    expect(slots.use[0]).toEqual(potion);
    expect(slots.equip[0]).toEqual(sword);
    expect(slots.etc[2]).toEqual(gel);
    expect(allItems(slots)).toEqual([sword, potion, gel]);
  });

  it.each([
    {
      name: 'a move empties the source and fills the target',
      before: [potion],
      change: [
        { tab: 'use' as const, slot: 0 },
        { tab: 'use' as const, slot: 3, item: { ...potion, slot: 3 } },
      ],
      want: { use: { 0: null, 3: { ...potion, slot: 3 } } },
    },
    {
      name: 'a stack grows in place',
      before: [potion],
      change: [{ tab: 'use' as const, slot: 0, item: { ...potion, quantity: 6 } }],
      want: { use: { 0: { ...potion, quantity: 6 } } },
    },
    {
      name: 'a discard removes the item',
      before: [gel],
      change: [{ tab: 'etc' as const, slot: 2 }],
      want: { etc: { 2: null } },
    },
    {
      name: 'a pickup fills a free slot',
      before: [],
      change: [{ tab: 'etc' as const, slot: 0, item: { ...gel, slot: 0 } }],
      want: { etc: { 0: { ...gel, slot: 0 } } },
    },
  ])('$name', ({ before, change, want }) => {
    inventory.getState().applyWelcome(welcomeWith(before));
    inventory.getState().applyInventoryChanged({ slots: change });
    const { slots } = inventory.getState();
    for (const [tab, expected] of Object.entries(want) as [
      'use' | 'etc',
      Record<string, ItemView | null>,
    ][]) {
      for (const [slot, item] of Object.entries(expected)) {
        expect(slots[tab][Number(slot)]).toEqual(item);
      }
    }
  });

  it('a slot never holds two items: the latest payload for a slot wins', () => {
    inventory.getState().applyWelcome(welcomeWith([potion]));
    inventory.getState().applyInventoryChanged({
      slots: [{ tab: 'use', slot: 0, item: { ...gel, tab: 'use', slot: 0 } }],
    });
    const { slots } = inventory.getState();
    expect(slots.use[0]?.item_id).toBe('i-gel');
    expect(allItems(slots)).toHaveLength(1);
  });

  it('a refused move leaves the store unchanged (nothing but a Response arrives)', () => {
    inventory.getState().applyWelcome(welcomeWith([potion, sword]));
    const before = inventory.getState().slots;
    // The server answered ok: false and sent no InventoryChanged.
    expect(inventory.getState().slots).toBe(before);
    expect(allItems(before)).toEqual([sword, potion]);
  });

  it('ignores a slot outside the tab, and clears', () => {
    inventory.getState().applyInventoryChanged({ slots: [{ tab: 'use', slot: 99, item: potion }] });
    expect(allItems(inventory.getState().slots)).toEqual([]);
    inventory.getState().applyWelcome(welcomeWith([potion]));
    inventory.getState().clear();
    expect(allItems(inventory.getState().slots)).toEqual([]);
  });
});
