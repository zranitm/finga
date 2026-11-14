import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create, fromJsonString } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { itemName } from '@/features/shared/items';
import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import {
  InventoryChangedSchema,
  ResponseSchema,
  ServerMessageSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { inventory } from '@/state/inventory';
import { dataTransfer } from '@/testing/data-transfer';
import { refusal } from '@/testing/refusal';
import { withoutKeys } from '@/testing/requests';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { InventoryWindow } from './InventoryWindow';

const sword = { itemId: 'i-sword', template: 'wooden_sword', tab: 'equip', slot: 0, quantity: 1 };
const cap = { itemId: 'i-cap', template: 'leather_cap', tab: 'equip', slot: 5, quantity: 1 };
const potion = { itemId: 'i-potion', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };

const welcome = create(WelcomeSchema, { token: 'tok', inventory: [sword, cap, potion] });
const ok = create(ResponseSchema, { ok: true });

/** Requests resolve or reject with the given answers, in order. */
function fakeSocket(answers: readonly (Data<'response'> | Error)[]) {
  const calls: SentRequest[] = [];
  const request = vi.fn((t: string, d: Record<string, unknown>) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? ok;
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

function setup(answers: readonly (Data<'response'> | Error)[] = []) {
  const { socket, calls } = fakeSocket(answers);
  const onClose = vi.fn();
  render(<InventoryWindow socket={socket} onClose={onClose} />);
  return { calls, onClose };
}

const cell = (name: RegExp | string) => screen.getByRole('gridcell', { name });

describe('InventoryWindow', () => {
  beforeEach(() => {
    resetStores();
    inventory.getState().applyWelcome(welcome);
  });

  it('shows the starter items from Welcome by tab, with names and quantities', async () => {
    setup();
    expect(cell('Wooden Sword x1')).toBeInTheDocument();
    expect(cell('Leather Cap x1')).toBeInTheDocument();
    expect(screen.getAllByRole('gridcell')).toHaveLength(24);
    await user.click(screen.getByRole('tab', { name: 'Use' }));
    expect(cell('Red Potion x5')).toHaveTextContent('5');
    expect(itemName('nothing_like_it')).toBe('nothing_like_it');
  });

  it('a drag between slots sends InventoryMove with the item, tab and slot, and the item waits in place as pending', async () => {
    const { calls } = setup([ok]);
    const carried = dataTransfer();
    fireEvent.dragStart(cell('Wooden Sword x1'), { dataTransfer: carried });
    fireEvent.drop(cell('empty slot 3'), { dataTransfer: carried });
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      t: 'inventoryMove',
      d: { itemId: 'i-sword', tab: 'equip', slot: 2 },
    });
    expect(String(calls[0]!.d.idempotencyKey)).toMatch(/^k-/);
    expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'true'); // still in slot 1
    await waitFor(() => expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false'));
    // Only InventoryChanged moves it.
    act(() =>
      inventory.getState().applyInventoryChanged(
        create(InventoryChangedSchema, {
          slots: [
            { tab: 'equip', slot: 0 },
            { tab: 'equip', slot: 2, item: { ...sword, slot: 2 } },
          ],
        }),
      ),
    );
    expect(screen.getAllByRole('gridcell')[2]).toHaveAccessibleName('Wooden Sword x1');
    expect(screen.getAllByRole('gridcell')[0]).toHaveAccessibleName('empty slot 1');
  });

  it('a timed-out request retried sends the same idempotency key', async () => {
    const { calls } = setup([new RequestError('timeout', 'no answer'), ok]);
    const carried = dataTransfer();
    fireEvent.dragStart(cell('Wooden Sword x1'), { dataTransfer: carried });
    fireEvent.drop(cell('empty slot 4'), { dataTransfer: carried });
    await user.click(cell('Wooden Sword x1'));
    await user.click(await screen.findByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1]!.d.idempotencyKey).toBe(calls[0]!.d.idempotencyKey);
    expect(calls[1]!.d.slot).toBe(3);
    await waitFor(() => expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false'));
  });

  it('a refused Response clears the pending mark and shows its words, the item never having moved', async () => {
    setup([refusal(ERR.SLOT_TAKEN)]);
    const carried = dataTransfer();
    fireEvent.dragStart(cell('Wooden Sword x1'), { dataTransfer: carried });
    fireEvent.drop(cell('Leather Cap x1'), { dataTransfer: carried });
    expect(await screen.findByRole('alert')).toHaveTextContent('That slot is taken.');
    expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false');
    expect(screen.getAllByRole('gridcell')[0]).toHaveAccessibleName('Wooden Sword x1');
  });

  it('a cross-tab drag never sends, and a drop on its own slot neither', async () => {
    const { calls } = setup();
    const carried = dataTransfer();
    fireEvent.dragStart(cell('Wooden Sword x1'), { dataTransfer: carried });
    await user.click(screen.getByRole('tab', { name: 'Use' }));
    fireEvent.drop(cell('empty slot 2'), { dataTransfer: carried });
    expect(calls).toHaveLength(0);
    expect(screen.getByRole('alert')).toHaveTextContent('Items stay in their tab.');
    await user.click(screen.getByRole('tab', { name: 'Equip' }));
    const again = dataTransfer();
    fireEvent.dragStart(cell('Wooden Sword x1'), { dataTransfer: again });
    fireEvent.drop(cell('Wooden Sword x1'), { dataTransfer: again });
    expect(calls).toHaveLength(0);
  });

  it('a double-click on an Equip-tab item equips it, the server picking the slot', async () => {
    const { calls } = setup([ok]);
    await user.dblClick(cell('Leather Cap x1'));
    expect(withoutKeys(calls)).toEqual([{ t: 'equip', d: { itemId: 'i-cap' } }]);
  });

  describe('potions', () => {
    const hurt = (hp: number) =>
      character
        .getState()
        .applyWelcome(create(WelcomeSchema, { character: { id: 'c-1', hp, maxHp: 200 } }));
    const useTab = () => user.click(screen.getByRole('tab', { name: 'Use' }));

    it('a double-click drinks one, and the Combat tab says what it restored once the world applied it', async () => {
      hurt(170);
      const { calls } = setup([ok]);
      await useTab();
      await user.dblClick(cell('Red Potion x5'));
      expect(withoutKeys(calls)).toEqual([
        { t: 'useItem', d: { itemId: 'i-potion', template: 'red_potion' } },
      ]);
      await waitFor(() =>
        expect(combatLog.getState().lines).toMatchObject([
          { kind: 'drank', template: 'red_potion', hp: 30 },
        ]),
      );
    });

    it('a selected potion has a Use button; equipment has none', async () => {
      hurt(20);
      const { calls } = setup([ok]);
      await user.click(cell('Wooden Sword x1'));
      expect(screen.queryByRole('button', { name: 'Use' })).not.toBeInTheDocument();
      await useTab();
      await user.click(cell('Red Potion x5'));
      await user.click(screen.getByRole('button', { name: 'Use' }));
      expect(withoutKeys(calls)).toEqual([
        { t: 'useItem', d: { itemId: 'i-potion', template: 'red_potion' } },
      ]);
    });

    it('at full HP one is refused here, and nothing is sent', async () => {
      hurt(200);
      const { calls } = setup();
      await useTab();
      await user.dblClick(cell('Red Potion x5'));
      expect(screen.getByRole('alert')).toHaveTextContent('Your HP is already full.');
      expect(calls).toHaveLength(0);
    });

    it('one the world refuses shows why, and the Combat tab says nothing', async () => {
      hurt(20);
      setup([refusal(ERR.CHARACTER_DEAD)]);
      await useTab();
      await user.dblClick(cell('Red Potion x5'));
      expect(await screen.findByRole('alert')).toHaveTextContent('You can’t do that while dead.');
      expect(combatLog.getState().lines).toEqual([]);
    });
  });

  it('an equip the server refuses shows why', async () => {
    setup([refusal(ERR.LEVEL_TOO_LOW)]);
    await user.dblClick(cell('Leather Cap x1'));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Your level is too low for that item.',
    );
  });

  it('a drag carries the item for the character window', () => {
    setup();
    const carried = dataTransfer();
    fireEvent.dragStart(cell('Leather Cap x1'), { dataTransfer: carried });
    expect(JSON.parse(carried.getData('application/x-finga-item'))).toEqual({
      from: 'inventory',
      itemId: 'i-cap',
    });
  });

  it('a worn item dropped on an Equip-tab slot unequips it into that slot; on another tab, into the first free one', async () => {
    const { calls } = setup([ok, ok]);
    const worn = () => {
      const carried = dataTransfer();
      carried.setData(
        'application/x-finga-item',
        JSON.stringify({ from: 'worn', equipSlot: 'weapon' }),
      );
      return carried;
    };
    fireEvent.drop(cell('empty slot 4'), { dataTransfer: worn() });
    expect(withoutKeys(calls)).toEqual([{ t: 'unequip', d: { equipSlot: 'weapon', toSlot: 3 } }]);
    await user.click(screen.getByRole('tab', { name: 'Use' })); // the first answer settles meanwhile
    fireEvent.drop(cell('empty slot 2'), { dataTransfer: worn() });
    expect(withoutKeys(calls)[1]).toEqual({ t: 'unequip', d: { equipSlot: 'weapon' } });
  });

  it('discard asks first, then sends InventoryDiscard with the whole stack and one key', async () => {
    const { calls } = setup([ok]);
    await user.click(screen.getByRole('tab', { name: 'Use' }));
    await user.click(cell('Red Potion x5'));
    await user.click(screen.getByRole('button', { name: 'Discard' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard Red Potion?');
    await user.click(screen.getByRole('button', { name: 'Keep' }));
    expect(calls).toHaveLength(0);
    await user.click(screen.getByRole('button', { name: 'Discard' }));
    await user.click(screen.getByRole('button', { name: 'Yes, discard' }));
    expect(calls[0]).toMatchObject({
      t: 'inventoryDiscard',
      d: { itemId: 'i-potion', quantity: 5 },
    });
    expect(String(calls[0]!.d.idempotencyKey)).toMatch(/^k-/);
  });

  describe('select mode', () => {
    const ticked = (name: string) => cell(name).getAttribute('aria-selected');
    const discardButton = () => screen.getByRole('button', { name: /^Discard \d+ items?$/ });

    it('Select turns the tab into tick boxes: a click ticks or unticks, the button counts them, and nothing drags or equips', async () => {
      const { calls } = setup();
      await user.click(screen.getByRole('button', { name: 'Select' }));
      expect(discardButton()).toHaveAccessibleName('Discard 0 items');
      expect(discardButton()).toBeDisabled();
      await user.click(cell('Wooden Sword x1'));
      await user.click(cell('Leather Cap x1'));
      expect([ticked('Wooden Sword x1'), ticked('Leather Cap x1')]).toEqual(['true', 'true']);
      expect(discardButton()).toHaveAccessibleName('Discard 2 items');
      await user.click(cell('Wooden Sword x1'));
      expect(ticked('Wooden Sword x1')).toBe('false');
      expect(discardButton()).toHaveAccessibleName('Discard 1 item');
      expect(cell('Leather Cap x1')).toHaveAttribute('draggable', 'false');
      await user.dblClick(cell('Wooden Sword x1'));
      expect(calls).toHaveLength(0);
      expect(screen.queryByRole('button', { name: 'Discard' })).not.toBeInTheDocument(); // no single-item bar meanwhile
    });

    it('switching tabs or leaving select mode clears the ticks', async () => {
      setup();
      await user.click(screen.getByRole('button', { name: 'Select' }));
      await user.click(cell('Wooden Sword x1'));
      await user.click(screen.getByRole('tab', { name: 'Use' }));
      expect(discardButton()).toHaveAccessibleName('Discard 0 items');
      await user.click(screen.getByRole('tab', { name: 'Equip' }));
      expect(ticked('Wooden Sword x1')).toBe('false');
      await user.click(cell('Leather Cap x1'));
      await user.click(screen.getByRole('button', { name: 'Cancel' }));
      expect(cell('Leather Cap x1')).not.toHaveAttribute('aria-selected');
      await user.click(screen.getByRole('button', { name: 'Select' }));
      expect(ticked('Leather Cap x1')).toBe('false');
    });

    it('one confirm sends every ticked item, stacks whole, in one request with one key; once done, select mode ends', async () => {
      let answer: ((r: Data<'response'>) => void) | undefined;
      const request = vi.fn<(t: string, d: Record<string, unknown>) => Promise<Data<'response'>>>(
        () => new Promise((resolve) => (answer = resolve)),
      );
      render(<InventoryWindow socket={{ request } as never} onClose={vi.fn()} />);
      await user.click(screen.getByRole('button', { name: 'Select' }));
      await user.click(cell('Leather Cap x1'));
      await user.click(cell('Wooden Sword x1'));
      await user.click(discardButton());
      expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard 2 items?');
      await user.click(screen.getByRole('button', { name: 'Keep' }));
      expect(request).not.toHaveBeenCalled();
      await user.click(discardButton());
      await user.click(screen.getByRole('button', { name: 'Yes, discard' }));
      expect(request).toHaveBeenCalledTimes(1);
      const [t, d] = request.mock.calls[0]!;
      expect([t, d.itemIds]).toEqual(['inventoryDiscardMany', ['i-sword', 'i-cap']]); // in slot order
      expect(String(d.idempotencyKey)).toMatch(/^k-/);
      expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'true');
      await user.click(cell('Wooden Sword x1'));
      expect(ticked('Wooden Sword x1')).toBe('true'); // held while on its way
      expect(discardButton()).toBeDisabled();
      act(() => answer?.(ok));
      expect(await screen.findByRole('button', { name: 'Select' })).toBeInTheDocument();
      expect(cell('Wooden Sword x1')).not.toHaveAttribute('aria-selected');
    });

    it('the confirm names every ticked item of four stars or more', async () => {
      const ring = (n: number) => ({
        itemId: `i-ring-${n}`,
        template: 'gold_ring',
        tab: 'equip',
        slot: 10 + n,
        quantity: 1,
      });
      const pendant = {
        itemId: 'i-pendant',
        template: 'starlight_pendant',
        tab: 'equip',
        slot: 20,
        quantity: 1,
      };
      inventory.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok',
          inventory: [sword, cap, ring(1), ring(2), pendant],
        }),
      );
      setup();
      await user.click(screen.getByRole('button', { name: 'Select' }));
      await user.click(cell('Wooden Sword x1'));
      await user.click(screen.getAllByRole('gridcell', { name: 'Gold Ring x1' })[0]!);
      await user.click(discardButton());
      expect(screen.getByRole('alertdialog')).toHaveTextContent(
        'Discard 2 items, including Gold Ring ★★★★?',
      );
      await user.click(screen.getAllByRole('gridcell', { name: 'Gold Ring x1' })[1]!);
      await user.click(cell('Starlight Pendant x1'));
      await user.click(discardButton());
      expect(screen.getByRole('alertdialog')).toHaveTextContent(
        'Discard 4 items, including Gold Ring ★★★★ x2 and Starlight Pendant ★★★★★?',
      );
    });

    it('a refused batch keeps the ticks and says why; a timed-out one retries with the same key', async () => {
      const { calls } = setup([
        refusal(ERR.ITEM_NOT_FOUND),
        new RequestError('timeout', 'no answer'),
        ok,
      ]);
      await user.click(screen.getByRole('button', { name: 'Select' }));
      await user.click(cell('Wooden Sword x1'));
      await user.click(discardButton());
      await user.click(screen.getByRole('button', { name: 'Yes, discard' }));
      expect(await screen.findByRole('alert')).toHaveTextContent('That item isn’t there any more.');
      expect(ticked('Wooden Sword x1')).toBe('true');
      await user.click(discardButton());
      await user.click(screen.getByRole('button', { name: 'Yes, discard' }));
      await user.click(await screen.findByRole('button', { name: 'Retry' }));
      await waitFor(() => expect(calls).toHaveLength(3));
      expect(calls[2]!.d.idempotencyKey).toBe(calls[1]!.d.idempotencyKey);
      expect(calls[2]!.d.itemIds).toEqual(['i-sword']);
      expect(await screen.findByRole('button', { name: 'Select' })).toBeInTheDocument();
    });
  });

  it('store and window agree after the golden InventoryChanged payload', () => {
    const golden = fromJsonString(
      ServerMessageSchema,
      readFileSync(
        path.resolve(
          process.cwd(),
          '..',
          'server',
          'testdata',
          'contracts',
          'InventoryChanged.json',
        ),
        'utf8',
      ),
    );
    if (golden.body.case !== 'inventoryChanged') throw new Error(golden.body.case);
    const changed: Data<'inventoryChanged'> = golden.body.value;
    inventory.getState().applyInventoryChanged(changed);
    setup();
    for (const s of changed.slots) {
      if (s.tab !== 'equip') {
        continue;
      }
      const name = s.item
        ? `${itemName(s.item.template)} x${s.item.quantity}`
        : `empty slot ${s.slot + 1}`;
      expect(screen.getAllByRole('gridcell')[s.slot]).toHaveAccessibleName(name);
    }
  });
});

describe('item cards', () => {
  beforeEach(() => {
    resetStores();
    const w = create(WelcomeSchema, {
      token: 't',
      character: { id: 'c1', name: 'Ayla', class: 'warrior', level: 1 },
      inventory: [
        {
          itemId: 'i-iron',
          template: 'iron_sword',
          tab: 'equip',
          slot: 0,
          quantity: 1,
          stats: { physicalAttack: 17 },
        },
        {
          itemId: 'i-cap',
          template: 'leather_cap',
          tab: 'equip',
          slot: 1,
          quantity: 1,
          stats: { physicalDefense: 4 },
        },
        potion,
      ],
      equipment: [
        {
          equipSlot: 'weapon',
          item: {
            itemId: 'i-sword',
            template: 'wooden_sword',
            quantity: 1,
            stats: { physicalAttack: 11 },
          },
        },
      ],
      inventoryRevision: 1n,
    });
    character.getState().applyWelcome(w);
    inventory.getState().applyWelcome(w);
  });

  it('hovering equipment shows its card beside the worn one, with what a swap changes and what the character misses', async () => {
    setup();
    await user.hover(cell('Iron Sword x1'));
    const hovered = screen.getByRole('group', { name: 'Iron Sword' });
    expect(hovered).toHaveTextContent('Physical attack 17 (16–20) +6');
    expect(within(hovered).getByText('Level 8')).toHaveClass('unmet');
    expect(within(hovered).getByText('Warrior')).not.toHaveClass('unmet');
    expect(screen.getByRole('group', { name: 'Worn: Wooden Sword' })).toHaveTextContent(
      'Physical attack 11 (10–12)',
    );
    await user.unhover(cell('Iron Sword x1'));
    expect(screen.queryByRole('group', { name: 'Iron Sword' })).toBeNull();
  });

  it('a hovered item that leaves its slot takes its card along, and what takes its place shows', async () => {
    setup();
    await user.hover(cell('Iron Sword x1'));
    const wooden = {
      itemId: 'i-sword',
      template: 'wooden_sword',
      quantity: 1,
      stats: { physicalAttack: 11 },
    };
    const iron = {
      itemId: 'i-iron',
      template: 'iron_sword',
      quantity: 1,
      stats: { physicalAttack: 17 },
    };
    // Worn by a double-click: the swap puts the old weapon in the slot under the pointer.
    act(() =>
      inventory.getState().applyInventoryChanged(
        create(InventoryChangedSchema, {
          revision: 2n,
          slots: [{ tab: 'equip', slot: 0, item: { ...wooden, tab: 'equip', slot: 0 } }],
          equipment: [{ equipSlot: 'weapon', item: iron }],
        }),
      ),
    );
    expect(screen.queryByRole('group', { name: 'Iron Sword' })).toBeNull();
    expect(screen.getByRole('group', { name: 'Wooden Sword' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'Worn: Iron Sword' })).toBeInTheDocument();
    act(() =>
      inventory
        .getState()
        .applyInventoryChanged(
          create(InventoryChangedSchema, { revision: 3n, slots: [{ tab: 'equip', slot: 0 }] }),
        ),
    );
    expect(screen.queryByRole('group', { name: /Sword/ })).toBeNull();
  });

  it('with nothing worn in its slot, the second card says so and every stat is a gain', async () => {
    setup();
    await user.hover(cell('Leather Cap x1'));
    expect(screen.getByRole('group', { name: 'Leather Cap' })).toHaveTextContent(
      'Physical defense 4 (3–4) +4',
    );
    expect(screen.getByRole('group', { name: 'Worn: nothing' })).toBeInTheDocument();
  });

  it('a Use item shows its card alone, and a drag takes the card away', async () => {
    setup();
    await user.click(screen.getByRole('tab', { name: 'Use' }));
    await user.hover(cell('Red Potion x5'));
    expect(screen.getByRole('group', { name: 'Red Potion' })).toHaveTextContent('Restores 50 HP');
    expect(screen.queryByRole('group', { name: /^Worn/ })).toBeNull();
    fireEvent.dragStart(cell('Red Potion x5'), { dataTransfer: dataTransfer() });
    expect(screen.queryByRole('group', { name: 'Red Potion' })).toBeNull();
  });
});
