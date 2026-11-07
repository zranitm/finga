import { create } from '@bufbuild/protobuf';
import type { MessageInitShape } from '@bufbuild/protobuf';
import { beforeEach, describe, expect, it } from 'vitest';

import { InventoryChangedSchema, ItemViewSchema, WelcomeSchema } from '@/protocol/messages';
import type { ItemView, InventorySlotSchema } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { allItems, inventory, isTab, SLOTS_PER_TAB } from './inventory';

const item = (itemId: string, template: string, tab: string, slot: number, quantity: number) =>
  create(ItemViewSchema, { itemId, template, tab, slot, quantity });
const potion = item('i-potion', 'red_potion', 'use', 0, 5);
const sword = item('i-sword', 'wooden_sword', 'equip', 0, 1);
const gel = item('i-gel', 'slime_gel', 'etc', 2, 2);

function welcomeWith(items: ItemView[]) {
  return create(WelcomeSchema, { token: 't', inventory: items, mapId: 'town' });
}

function changed(slots: MessageInitShape<typeof InventorySlotSchema>[]) {
  return create(InventoryChangedSchema, { slots });
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
        { tab: 'use', slot: 0 },
        { tab: 'use', slot: 3, item: { ...potion, slot: 3 } },
      ],
      want: { use: { 0: null, 3: { ...potion, slot: 3 } } },
    },
    {
      name: 'a stack grows in place',
      before: [potion],
      change: [{ tab: 'use', slot: 0, item: { ...potion, quantity: 6 } }],
      want: { use: { 0: { ...potion, quantity: 6 } } },
    },
    {
      name: 'a discard removes the item',
      before: [gel],
      change: [{ tab: 'etc', slot: 2 }],
      want: { etc: { 2: null } },
    },
    {
      name: 'a pickup fills a free slot',
      before: [],
      change: [{ tab: 'etc', slot: 0, item: { ...gel, slot: 0 } }],
      want: { etc: { 0: { ...gel, slot: 0 } } },
    },
  ])('$name', ({ before, change, want }) => {
    inventory.getState().applyWelcome(welcomeWith(before));
    inventory.getState().applyInventoryChanged(changed(change));
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
    inventory
      .getState()
      .applyInventoryChanged(
        changed([{ tab: 'use', slot: 0, item: { ...gel, tab: 'use', slot: 0 } }]),
      );
    const { slots } = inventory.getState();
    expect(slots.use[0]?.itemId).toBe('i-gel');
    expect(allItems(slots)).toHaveLength(1);
  });

  it('a refused move leaves the store unchanged (nothing but a Response arrives)', () => {
    inventory.getState().applyWelcome(welcomeWith([potion, sword]));
    const before = inventory.getState().slots;
    // The server answered ok: false and sent no InventoryChanged.
    expect(inventory.getState().slots).toBe(before);
    expect(allItems(before)).toEqual([sword, potion]);
  });

  it('ignores a slot outside the tab or in a tab this client does not know, and clears', () => {
    inventory.getState().applyInventoryChanged(changed([{ tab: 'use', slot: 99, item: potion }]));
    inventory.getState().applyInventoryChanged(changed([{ tab: 'cash', slot: 0, item: potion }]));
    expect(allItems(inventory.getState().slots)).toEqual([]);
    expect([isTab('equip'), isTab('cash')]).toEqual([true, false]);
    inventory.getState().applyWelcome(welcomeWith([potion]));
    inventory.getState().clear();
    expect(allItems(inventory.getState().slots)).toEqual([]);
  });
});

describe('inventory revisions', () => {
  beforeEach(() => {
    resetStores();
  });

  const stack = item('i-gel', 'slime_gel', 'etc', 1, 3);
  const welcomeAt = (items: ItemView[], inventoryRevision: bigint) =>
    create(WelcomeSchema, { token: 't', inventory: items, mapId: 'town', inventoryRevision });
  const changedAt = (slots: MessageInitShape<typeof InventorySlotSchema>[], revision: bigint) =>
    create(InventoryChangedSchema, { slots, revision });

  it('an older change arriving after a newer one leaves the newer slots be', () => {
    // A pickup (revision 2) grows the stack in slot 1, then a move (revision 3) takes it to slot 5; the move's answer comes first.
    inventory.getState().applyWelcome(welcomeAt([stack], 1n));
    inventory.getState().applyInventoryChanged(
      changedAt(
        [
          { tab: 'etc', slot: 1 },
          { tab: 'etc', slot: 5, item: { ...stack, slot: 5, quantity: 4 } },
        ],
        3n,
      ),
    );
    inventory
      .getState()
      .applyInventoryChanged(
        changedAt([{ tab: 'etc', slot: 1, item: { ...stack, quantity: 4 } }], 2n),
      );
    const { slots } = inventory.getState();
    expect(slots.etc[1]).toBeNull();
    expect(slots.etc[5]?.quantity).toBe(4);
    expect(allItems(slots)).toHaveLength(1);
  });

  it('an older change still sets the slots no newer one touched', () => {
    inventory.getState().applyWelcome(welcomeAt([stack], 1n));
    inventory.getState().applyInventoryChanged(
      changedAt(
        [
          { tab: 'etc', slot: 1 },
          { tab: 'etc', slot: 5, item: { ...stack, slot: 5 } },
        ],
        3n,
      ),
    );
    inventory.getState().applyInventoryChanged(
      changedAt(
        [
          { tab: 'etc', slot: 1, item: stack },
          { tab: 'etc', slot: 7, item: { ...gel, slot: 7 } },
        ],
        2n,
      ),
    );
    const { slots } = inventory.getState();
    expect(slots.etc[1]).toBeNull();
    expect(slots.etc[5]?.itemId).toBe('i-gel');
    expect(slots.etc[7]).toEqual({ ...gel, slot: 7 });
  });

  it('a change the Welcome already shows is skipped', () => {
    inventory.getState().applyWelcome(welcomeAt([gel], 5n));
    inventory.getState().applyInventoryChanged(changedAt([{ tab: 'etc', slot: 2 }], 4n));
    expect(inventory.getState().slots.etc[2]).toEqual(gel);
  });

  it('a change stored before revisions applies as it comes, and leaves the revision of its slots as it was', () => {
    inventory.getState().applyWelcome(welcomeAt([potion], 5n));
    inventory
      .getState()
      .applyInventoryChanged(
        changedAt([{ tab: 'use', slot: 0, item: { ...potion, quantity: 6 } }], 0n),
      );
    expect(inventory.getState().slots.use[0]?.quantity).toBe(6);
    inventory
      .getState()
      .applyInventoryChanged(
        changedAt([{ tab: 'use', slot: 0, item: { ...potion, quantity: 2 } }], 4n),
      );
    expect(inventory.getState().slots.use[0]?.quantity).toBe(6);
  });

  it('a new Welcome, and a clear, start every slot over', () => {
    inventory.getState().applyWelcome(welcomeAt([gel], 9n));
    inventory.getState().applyWelcome(welcomeAt([gel], 2n));
    inventory.getState().applyInventoryChanged(changedAt([{ tab: 'etc', slot: 2 }], 3n));
    expect(inventory.getState().slots.etc[2]).toBeNull();
    inventory.getState().applyWelcome(welcomeAt([gel], 9n));
    inventory.getState().clear();
    inventory.getState().applyInventoryChanged(changedAt([{ tab: 'etc', slot: 2, item: gel }], 1n));
    expect(inventory.getState().slots.etc[2]).toEqual(gel);
  });
});
