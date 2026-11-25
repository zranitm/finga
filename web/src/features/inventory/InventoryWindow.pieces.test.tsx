import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { CharacterViewSchema, ResponseSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { wardrobe } from '@/state/wardrobe';
import { refusal } from '@/testing/refusal';
import { withoutKeys } from '@/testing/requests';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { InventoryWindow } from './InventoryWindow';

const plate = {
  itemId: 'i-plate',
  template: 'piece.knight_plate',
  tab: 'etc',
  slot: 0,
  quantity: 1,
};
const wand = { itemId: 'i-wand', template: 'piece.oak_wand', tab: 'etc', slot: 1, quantity: 1 };
const gel = { itemId: 'i-gel', template: 'slime_gel', tab: 'etc', slot: 2, quantity: 3 };
const ok = create(ResponseSchema, { ok: true });

/** The inventory open on a Warrior's Etc tab, the server answering with answer. */
async function setup(answer: () => Promise<unknown> = () => Promise.resolve(ok)) {
  const calls: SentRequest[] = [];
  const request = vi.fn((t: string, d: Record<string, unknown>) => {
    calls.push({ t, d });
    return answer();
  });
  render(<InventoryWindow socket={{ request } as never} onClose={vi.fn()} />);
  await user.click(screen.getByRole('tab', { name: 'Etc' }));
  return { calls };
}

const select = (name: string) => user.click(screen.getByRole('gridcell', { name }));
const button = (name: string) => screen.queryByRole('button', { name });

describe('a wardrobe piece in the inventory (outfits.md section 7)', () => {
  beforeEach(() => {
    resetStores();
    act(() => {
      const welcome = create(WelcomeSchema, {
        token: 't',
        character: create(CharacterViewSchema, { id: 'c1', name: 'Ayla', class: 'warrior' }),
        inventory: [plate, wand, gel],
      });
      character.getState().applyWelcome(welcome);
      inventory.getState().applyWelcome(welcome);
      wardrobe.getState().collect(['knight_helm']);
    });
  });

  it('is named and starred as its piece, and offers Preview and Add to wardrobe; an item that holds none offers neither', async () => {
    await setup();
    await select("Knight's Plate x1");
    expect(button('Preview')).toBeEnabled();
    expect(button('Add to wardrobe')).toBeEnabled();
    await select('Slime Gel x3');
    expect([button('Preview'), button('Add to wardrobe')]).toEqual([null, null]);
  });

  it('adds only once asked; the answer leaves the wardrobe and its notice to PiecesAdded', async () => {
    const { calls } = await setup();
    await select("Knight's Plate x1");
    await user.click(button('Add to wardrobe')!);
    const ask = screen.getByRole('alertdialog', { name: 'Add to wardrobe' });
    expect(ask).toHaveTextContent("Add Knight's Plate to your wardrobe? It can’t be traded after.");
    await user.click(within(ask).getByRole('button', { name: 'Keep' }));
    expect(calls).toEqual([]);
    await user.click(button('Add to wardrobe')!);
    await user.click(screen.getByRole('button', { name: 'Yes, add' }));
    expect(withoutKeys(calls)).toEqual([{ t: 'addToWardrobe', d: { itemId: 'i-plate' } }]);
    expect(screen.queryByRole('alertdialog')).toBeNull();
    await vi.waitFor(() => expect(button('Add to wardrobe')).toBeEnabled()); // answered
    expect(wardrobe.getState().collected).toEqual(new Set(['knight_helm']));
    expect(toasts.getState().toasts).toEqual([]);
    act(() => wardrobe.getState().add('knight_plate')); // as PiecesAdded tells
    expect(button('Add to wardrobe')).toBeDisabled();
    expect(screen.getByText('Already in your wardrobe')).toBeInTheDocument();
  });

  it('shows a refusal, and the wardrobe and notices hear nothing', async () => {
    await setup(() => Promise.reject(refusal(ERR.PIECE_COLLECTED)));
    await select("Knight's Plate x1");
    await user.click(button('Add to wardrobe')!);
    await user.click(screen.getByRole('button', { name: 'Yes, add' }));
    expect(await screen.findByText(refusal(ERR.PIECE_COLLECTED).message)).toBeInTheDocument();
    expect(wardrobe.getState().collected).toEqual(new Set(['knight_helm']));
    expect(toasts.getState().toasts).toEqual([]);
  });

  it('greys Add for a piece already collected', async () => {
    act(() => wardrobe.getState().collect(['knight_plate']));
    await setup();
    await select("Knight's Plate x1");
    expect(button('Add to wardrobe')).toBeDisabled();
    expect(screen.getByText('Already in your wardrobe')).toBeInTheDocument();
  });

  it('previews by opening the wardrobe with the piece handed to it, but not a weapon look of a kind the class can’t carry, which it can still add', async () => {
    await setup();
    await select("Knight's Plate x1");
    await user.click(button('Preview')!);
    expect(wardrobe.getState().trying).toBe('knight_plate');
    expect(ui.getState().windows.at(-1)).toBe('wardrobe'); // opened on top
    await select('Oak Wand x1');
    expect(button('Preview')).toBeDisabled();
    expect(screen.getByText('Your class can’t carry it')).toBeInTheDocument();
    expect(button('Add to wardrobe')).toBeEnabled(); // the wardrobe is the account's: another character can
  });
});
