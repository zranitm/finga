import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create, fromJsonString } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import {
  InventoryChangedSchema,
  ResponseSchema,
  ServerMessageSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { inventory } from '@/state/inventory';
import { resetStores } from '@/testing/reset-stores';

import { InventoryWindow, itemName } from './InventoryWindow';

const sword = { itemId: 'i-sword', template: 'wooden_sword', tab: 'equip', slot: 0, quantity: 1 };
const cap = { itemId: 'i-cap', template: 'leather_cap', tab: 'equip', slot: 5, quantity: 1 };
const potion = { itemId: 'i-potion', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };

const welcome = create(WelcomeSchema, { token: 'tok', inventory: [sword, cap, potion] });
const ok = create(ResponseSchema, { ok: true });

interface Request {
  t: string;
  d: Record<string, unknown>;
}

/** Requests resolve or reject with the given answers, in order. */
function fakeSocket(answers: readonly (Data<'response'> | Error)[]) {
  const calls: Request[] = [];
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
    await userEvent.click(screen.getByRole('tab', { name: 'Use' }));
    expect(cell('Red Potion x5')).toHaveTextContent('5');
    expect(itemName('nothing_like_it')).toBe('nothing_like_it');
  });

  it('a drag between slots sends InventoryMove with the item, tab and slot, and the item waits in place as pending', async () => {
    const { calls } = setup([ok]);
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('empty slot 3'));
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
    fireEvent.dragStart(cell('Wooden Sword x1'));
    fireEvent.drop(cell('empty slot 4'));
    await userEvent.click(cell('Wooden Sword x1'));
    await userEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1]!.d.idempotencyKey).toBe(calls[0]!.d.idempotencyKey);
    expect(calls[1]!.d.slot).toBe(3);
    await waitFor(() => expect(cell('Wooden Sword x1')).toHaveAttribute('aria-busy', 'false'));
  });

  it('a refused Response clears the pending mark and shows the reason, the item never having moved', async () => {
    setup([new RequestError('slot_taken', 'that slot is taken')]);
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
    const { calls } = setup([ok]);
    await userEvent.click(screen.getByRole('tab', { name: 'Use' }));
    await userEvent.click(cell('Red Potion x5'));
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard Red Potion?');
    await userEvent.click(screen.getByRole('button', { name: 'Keep' }));
    expect(calls).toHaveLength(0);
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }));
    await userEvent.click(screen.getByRole('button', { name: 'Yes, discard' }));
    expect(calls[0]).toMatchObject({
      t: 'inventoryDiscard',
      d: { itemId: 'i-potion', quantity: 5 },
    });
    expect(String(calls[0]!.d.idempotencyKey)).toMatch(/^k-/);
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
