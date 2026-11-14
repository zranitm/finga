import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { ERR } from '@/protocol/errors';
import { ItemViewSchema } from '@/protocol/messages';
import type { ItemView } from '@/protocol/messages';
import { SLOTS_PER_TAB } from '@/state/inventory';
import type { Slots, Tab } from '@/state/inventory';

import { fitsNow, freeText, fullTab, lineOf, toFree, trainerWeapon } from './room';
import type { Line } from './room';

const tabOf: Record<string, Tab> = {
  leather_cap: 'equip',
  red_potion: 'use',
  orange_potion: 'use',
  slime_gel: 'etc',
  bat_wing: 'etc',
  mushroom_cap: 'etc',
};

/** A bag holding the given stacks in its first slots; filler fills each tab named up to its last slot. */
function bag(held: readonly [string, number][], full: readonly Tab[] = []): Slots {
  const slots: Record<Tab, (ItemView | null)[]> = {
    equip: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    use: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
    etc: Array<ItemView | null>(SLOTS_PER_TAB).fill(null),
  };
  const put = (template: string, quantity: number) => {
    const tab = tabOf[template]!;
    const slot = slots[tab].indexOf(null);
    slots[tab][slot] = create(ItemViewSchema, {
      itemId: `${tab}-${slot}`,
      template,
      tab,
      slot,
      quantity,
    });
  };
  held.forEach(([t, n]) => put(t, n));
  const filler: Record<Tab, string> = {
    equip: 'leather_cap',
    use: 'orange_potion',
    etc: 'mushroom_cap',
  };
  for (const tab of full) {
    while (slots[tab].includes(null)) {
      put(filler[tab], 1);
    }
  }
  return slots;
}

const line = (template: string, quantity: number): Line => lineOf(template, quantity)!;
const cap = line('leather_cap', 1);

describe('room for a reward', () => {
  it('a full tab: the reward does not fit, and one slot there is to free', () => {
    const slots = bag([], ['equip']);
    expect(fitsNow(slots, [], [cap])).toBe(false);
    expect(toFree(slots, [], [cap], [])).toEqual({ equip: 1 });
    expect(fitsNow(bag([['leather_cap', 1]]), [], [cap])).toBe(true);
  });

  it('two full tabs name each, and the count of each', () => {
    const slots = bag([], ['equip', 'use']);
    const short = toFree(slots, [], [cap, cap, line('red_potion', 10)], []);
    expect(short).toEqual({ equip: 2, use: 1 });
    expect(freeText(short, 'this reward')).toBe(
      'Free 2 slots in your Equip tab and 1 in your Use tab to take this reward.',
    );
    expect(freeText({ equip: 1, use: 2, etc: 1 }, 'this reward')).toBe(
      'Free 1 slot in your Equip tab, 2 in your Use tab and 1 in your Etc tab to take this reward.',
    );
  });

  it('a take frees a slot only when it empties a stack, the smallest first', () => {
    const wing = line('bat_wing', 1);
    const gel = (n: number) => line('slime_gel', n);
    // Etc full but for the gel; the reward is an Etc item of its own.
    expect(fitsNow(bag([['slime_gel', 3]], ['etc']), [gel(3)], [wing])).toBe(true);
    expect(toFree(bag([['slime_gel', 10]], ['etc']), [gel(3)], [wing], [])).toEqual({ etc: 1 });
    // Five from stacks of 10 and 3 empties the 3 first.
    expect(
      fitsNow(
        bag(
          [
            ['slime_gel', 10],
            ['slime_gel', 3],
          ],
          ['etc'],
        ),
        [gel(5)],
        [wing],
      ),
    ).toBe(true);
  });

  it('a stackable reward fills its stacks first, and only what is left needs a slot', () => {
    expect(fitsNow(bag([['red_potion', 90]], ['use']), [], [line('red_potion', 10)])).toBe(true);
    expect(toFree(bag([['red_potion', 95]], ['use']), [], [line('red_potion', 10)], [])).toEqual({
      use: 1,
    });
  });

  it('drops waiting for room are counted in, as each can take a freed slot first, but never block what fits now', () => {
    const roomForOne = bag(
      Array.from({ length: SLOTS_PER_TAB - 1 }, () => ['leather_cap', 1] as [string, number]),
    );
    expect(fitsNow(roomForOne, [], [cap])).toBe(true);
    expect(toFree(roomForOne, [], [cap], [cap])).toEqual({ equip: 1 });
    expect(toFree(bag([], ['equip']), [], [cap], [cap, cap])).toEqual({ equip: 3 });
    // A stackable one fills the stack first: 85 and 10 waiting leave room for 5 of the 10 given, and 5 waiting for all of them.
    const potions = bag([['red_potion', 85]], ['use']);
    expect(fitsNow(potions, [], [line('red_potion', 10)])).toBe(true);
    expect(toFree(potions, [], [line('red_potion', 10)], [line('red_potion', 10)])).toEqual({
      use: 1,
    });
    expect(toFree(potions, [], [line('red_potion', 10)], [line('red_potion', 5)])).toEqual({});
  });

  it('a drop waiting in a tab the reward does not use never counts', () => {
    const short = toFree(bag([], ['equip', 'etc']), [], [trainerWeapon], [line('bat_wing', 1)]);
    expect(short).toEqual({ equip: 1 });
    expect(freeText(short, 'your weapon')).toBe(
      'Free 1 slot in your Equip tab to take your weapon.',
    );
    // In the reward's own tab it does: the cap takes the first slot freed.
    expect(toFree(bag([], ['equip']), [], [cap], [cap])).toEqual({ equip: 2 });
    // Nor does one in a tab the reward only stacks into.
    expect(
      toFree(
        bag([['red_potion', 50]], ['use']),
        [],
        [line('red_potion', 10)],
        [line('orange_potion', 1)],
      ),
    ).toEqual({});
  });

  it('a waiting drop lands whole or not at all, and a reward can stack onto it', () => {
    // Its 5 land in the one slot freed, and the 10 given join them.
    expect(toFree(bag([], ['use']), [], [line('red_potion', 10)], [line('red_potion', 5)])).toEqual(
      {
        use: 1,
      },
    );
    // 85 held and 20 waiting: with no slot none of the 20 land, so the 10 given still fill the stack.
    const potions = bag([['red_potion', 85]], ['use']);
    expect(toFree(potions, [], [line('red_potion', 10)], [line('red_potion', 20)])).toEqual({});
    // With one slot freed the 20 land, 15 on the stack and 5 in it, and 20 given join those 5.
    expect(toFree(potions, [], [line('red_potion', 20)], [line('red_potion', 20)])).toEqual({
      use: 1,
    });
  });

  it('a tab a refusal names needs at least one slot, though the client counts room there', () => {
    expect(toFree(bag([]), [], [cap], [], 'equip')).toEqual({ equip: 1 });
    expect(toFree(bag([['leather_cap', 1]]), [], [line('red_potion', 1)], [], 'use')).toEqual({
      use: 1,
    });
  });

  it("a trainer's weapon is one Equip-tab item", () => {
    const short = toFree(bag([], ['equip']), [], [trainerWeapon], []);
    expect(short).toEqual({ equip: 1 });
    expect(freeText(short, 'your weapon')).toBe(
      'Free 1 slot in your Equip tab to take your weapon.',
    );
  });

  it('reads the tab a refusal names, and nothing from any other', () => {
    expect([ERR.EQUIP_TAB_FULL, ERR.USE_TAB_FULL, ERR.ETC_TAB_FULL].map(fullTab)).toEqual([
      'equip',
      'use',
      'etc',
    ]);
    expect(fullTab(ERR.QUEST_NOT_READY)).toBeUndefined();
    expect(fullTab('timeout')).toBeUndefined();
  });
});
