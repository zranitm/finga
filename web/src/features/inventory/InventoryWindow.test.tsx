import { readFileSync } from 'node:fs';
import path from 'node:path';

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { inventory } from '@/state/inventory';
import { resetStores } from '@/testing/reset-stores';

import { InventoryWindow, itemName } from './InventoryWindow';

const sword = {
  item_id: 'i-sword',
  template: 'wooden_sword',
  tab: 'equip',
  slot: 0,
  quantity: 1,
} as const;
const cap = {
  item_id: 'i-cap',
  template: 'leather_cap',
  tab: 'equip',
  slot: 5,
  quantity: 1,
} as const;
const potion = {
  item_id: 'i-potion',
  template: 'red_potion',
  tab: 'use',
  slot: 0,
  quantity: 5,
} as const;

const welcome = {
  token: 'tok',
  character: {
    id: 'c-ayla',
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
  inventory: [sword, cap, potion],
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

interface Request {
  t: string;
  d: Record<string, unknown>;
}

/** A socket double whose requests resolve or reject as the test decides, in order. */
function fakeSocket(answers: readonly (Data<'Response'> | Error)[]) {
  const calls: Request[] = [];
  const request = vi.fn((t: string, d: Record<string, unknown>) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

function setup(answers: readonly (Data<'Response'> | Error)[] = []) {
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
    await userEvent.click(screen.getByRole('tab', { name: 'Use' }));
    expect(cell('Red Potion x5')).toHaveTextContent('5');
    expect(itemName('nothing_like_it')).toBe('nothing_like_it');
  });

  it('a drag between slots sends InventoryMove with the item, tab and slot, and the item waits in place as pending', async () => {
    const { calls } = setup([{ ok: true }]);
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('empty slot 3'));
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      t: 'InventoryMove',
      d: { item_id: 'i-sword', tab: 'equip', slot: 2 },
    });
    expect(String(calls[0]!.d.idempotency_key)).toMatch(/^k-/);
    expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'true'); // still in slot 1
    await waitFor(() => expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false'));
    // Only InventoryChanged moves it.
    act(() =>
      inventory.getState().applyInventoryChanged({
        slots: [
          { tab: 'equip', slot: 0 },
          { tab: 'equip', slot: 2, item: { ...sword, slot: 2 } },
        ],
      }),
    );
    expect(screen.getAllByRole('gridcell')[2]).toHaveAccessibleName('Wooden Sword x1');
    expect(screen.getAllByRole('gridcell')[0]).toHaveAccessibleName('empty slot 1');
  });

  it('a timed-out request retried sends the same idempotency key', async () => {
    const { calls } = setup([new RequestError('timeout', 'no answer'), { ok: true }]);
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('empty slot 4'));
    await userEvent.click(cell('Wooden Sword x1'));
    await userEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1]!.d.idempotency_key).toBe(calls[0]!.d.idempotency_key);
    expect(calls[1]!.d.slot).toBe(3);
    await waitFor(() => expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false'));
  });

  it('a refused Response clears the pending mark and shows the reason, the item never having moved', async () => {
    setup([new RequestError('invalid_argument', 'that slot is taken')]);
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('Leather Cap x1'));
    expect(await screen.findByRole('alert')).toHaveTextContent('that slot is taken');
    expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false');
    expect(screen.getAllByRole('gridcell')[0]).toHaveAccessibleName('Wooden Sword x1');
  });

  it('a cross-tab drag never sends, and a drop on its own slot neither', async () => {
    const { calls } = setup();
    fireEvent.dragStart(cell('Wooden Sword x1'));
    await userEvent.click(screen.getByRole('tab', { name: 'Use' }));
    fireEvent.drop(cell('empty slot 2'));
    expect(calls).toHaveLength(0);
    expect(screen.getByRole('alert')).toHaveTextContent('Items stay in their tab.');
    await userEvent.click(screen.getByRole('tab', { name: 'Equip' }));
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('Wooden Sword x1'));
    expect(calls).toHaveLength(0);
  });

  it('discard asks first, then sends InventoryDiscard with the whole stack and one key', async () => {
    const { calls } = setup([{ ok: true }]);
    await userEvent.click(screen.getByRole('tab', { name: 'Use' }));
    await userEvent.click(cell('Red Potion x5'));
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard Red Potion?');
    await userEvent.click(screen.getByRole('button', { name: 'Keep' }));
    expect(calls).toHaveLength(0);
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    await userEvent.click(screen.getByRole('button', { name: 'Yes, discard' }));
    expect(calls[0]).toMatchObject({
      t: 'InventoryDiscard',
      d: { item_id: 'i-potion', quantity: 5 },
    });
    expect(String(calls[0]!.d.idempotency_key)).toMatch(/^k-/);
  });

  it('store and window agree after the golden InventoryChanged payload', () => {
    const golden = JSON.parse(
      readFileSync(
        path.resolve(process.cwd(), '..', 'testdata', 'contracts', 'InventoryChanged.json'),
        'utf8',
      ),
    ) as { d: Data<'InventoryChanged'> };
    inventory.getState().applyInventoryChanged(golden.d);
    setup();
    for (const s of golden.d.slots) {
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
