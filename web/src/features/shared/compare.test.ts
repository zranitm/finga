import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import type { ItemTemplate } from '@/components/ItemCard';
import { CharacterViewSchema, ItemStatsSchema, ItemViewSchema } from '@/protocol/messages';
import { equipSlotIds } from '@/state/inventory';
import type { Equipment } from '@/state/inventory';

import { changes, slotFor, unmet } from './compare';

const nothing = Object.fromEntries(equipSlotIds.map((id) => [id, null])) as Equipment;
const ring = create(ItemViewSchema, { itemId: 'i-ring', template: 'opal_ring', quantity: 1 });
const stats = (s: Record<string, number>) => create(ItemStatsSchema, s);

describe('slotFor', () => {
  it.each([
    {
      name: 'a weapon goes to the weapon slot, worn or not',
      kind: 'weapon',
      equipment: nothing,
      want: 'weapon',
    },
    {
      name: 'a ring to the first ring slot when both are empty',
      kind: 'ring',
      equipment: nothing,
      want: 'ring_1',
    },
    {
      name: 'to the empty one when the first is worn',
      kind: 'ring',
      equipment: { ...nothing, ring_1: ring },
      want: 'ring_2',
    },
    {
      name: 'and back to the first when both are worn',
      kind: 'ring',
      equipment: { ...nothing, ring_1: ring, ring_2: ring },
      want: 'ring_1',
    },
    { name: 'a kind no slot takes goes nowhere', kind: 'tail', equipment: nothing, want: null },
  ])('$name', ({ kind, equipment, want }) => {
    expect(slotFor(kind, equipment)).toBe(want);
  });
});

describe('changes', () => {
  it('is the hovered roll less the worn one, per stat, leaving out what stays the same', () => {
    expect(
      changes(stats({ physicalAttack: 18, str: 2 }), stats({ physicalAttack: 16, str: 2 })),
    ).toEqual({
      physicalAttack: 2,
    });
    expect(changes(stats({ physicalAttack: 14 }), stats({ physicalAttack: 16 }))).toEqual({
      physicalAttack: -2,
    });
  });

  it('counts a stat only one of the two has in full', () => {
    expect(changes(stats({ physicalDefense: 4 }), stats({ maxHp: 30, critRate: 5 }))).toEqual({
      physicalDefense: 4,
      maxHp: -30,
      critRate: -5,
    });
  });

  it('with nothing worn, every stat is a gain; with no roll at all, nothing changes', () => {
    expect(changes(stats({ physicalDefense: 4, agi: 1 }), undefined)).toEqual({
      physicalDefense: 4,
      agi: 1,
    });
    expect(changes(undefined, undefined)).toEqual({});
  });
});

describe('unmet', () => {
  const bow: ItemTemplate = {
    id: 'hunting_bow',
    name: 'Hunting Bow',
    tab: 'equip',
    slot: 'weapon',
    class: 'bowman',
    min_level: 8,
    stars: 2,
    description: '',
  };
  const cap: ItemTemplate = {
    id: 'leather_cap',
    name: 'Leather Cap',
    tab: 'equip',
    slot: 'hat',
    min_level: 1,
    stars: 1,
    description: '',
  };
  const warrior = (level: number) => create(CharacterViewSchema, { class: 'warrior', level });

  it.each([
    {
      name: 'a level-1 warrior misses both of a level-8 bow',
      t: bow,
      c: warrior(1),
      want: { level: true, class: true },
    },
    {
      name: 'a level-8 warrior misses only its class',
      t: bow,
      c: warrior(8),
      want: { level: false, class: true },
    },
    {
      name: 'an item for every class asks only the level',
      t: cap,
      c: warrior(1),
      want: { level: false, class: false },
    },
    {
      name: 'without a character nothing is marked',
      t: bow,
      c: null,
      want: { level: false, class: false },
    },
  ])('$name', ({ t, c, want }) => {
    expect(unmet(t, c)).toEqual(want);
  });
});
