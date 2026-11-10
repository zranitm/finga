import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import {
  InventoryChangedSchema,
  LevelUpSchema,
  ResponseSchema,
  StatsChangedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import { dataTransfer } from '@/testing/data-transfer';
import { withoutKeys } from '@/testing/requests';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';

import { CharacterWindow } from './CharacterWindow';

const sheet = {
  primary: { str: 13, dex: 5, int: 4, agi: 5, vit: 10 },
  primaryWorn: { str: 1 },
  derived: {
    physicalAttack: 31.5,
    physicalDefense: 3,
    accuracy: 15,
    evasion: 8.75,
    critRate: 0.065,
    critDamage: 1.5,
    maxHp: 204,
    maxMp: 28,
  },
  derivedWorn: { physicalAttack: 11.5, critRate: 0.01 },
};
const ayla = {
  id: 'c1',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 180,
  maxHp: 204,
  mp: 28,
  maxMp: 28,
  sheet,
};
const sword = {
  itemId: 'i-sword',
  template: 'wooden_sword',
  quantity: 1,
  stats: { physicalAttack: 10, str: 1 },
};
const welcome = create(WelcomeSchema, {
  token: 't',
  character: ayla,
  equipment: [{ equipSlot: 'weapon', item: sword }],
  inventoryRevision: 1n,
});
const ok = create(ResponseSchema, { ok: true });

function setup(answers: readonly (Data<'response'> | Error)[] = []) {
  const calls: SentRequest[] = [];
  const request = vi.fn((t: string, d: Record<string, unknown>) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? ok;
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  const onClose = vi.fn();
  render(<CharacterWindow socket={{ request } as never} onClose={onClose} />);
  return { calls, onClose };
}

const slot = (name: string) => screen.getByRole('button', { name });
const stat = (table: string, name: string) =>
  within(screen.getByRole('table', { name: table })).getByRole('rowheader', { name }).nextSibling;

describe('CharacterWindow', () => {
  beforeEach(() => {
    resetStores();
    character.getState().applyWelcome(welcome);
    inventory.getState().applyWelcome(welcome);
  });

  it('shows who the character is, its EXP, HP and MP', () => {
    setup();
    const w = screen.getByRole('dialog', { name: 'Character' });
    expect(w).toHaveTextContent('Ayla');
    expect(w).toHaveTextContent('Warrior · Lv 1');
    expect(w).toHaveTextContent('EXP10 / 20');
    expect(w).toHaveTextContent('HP180 / 204');
    expect(w).toHaveTextContent('MP28 / 28');
  });

  it('shows the ten slots: the worn sword framed in its star colour, every empty slot by name', () => {
    setup();
    const slots = within(screen.getByRole('group', { name: 'Equipment' })).getAllByRole('button');
    expect(slots.map((s) => s.getAttribute('aria-label'))).toEqual([
      'Weapon: Wooden Sword',
      'Hat, empty',
      'Top, empty',
      'Bottom, empty',
      'Gloves, empty',
      'Shoes, empty',
      'Necklace, empty',
      'Earrings, empty',
      'Ring 1, empty',
      'Ring 2, empty',
    ]);
    expect(slot('Weapon: Wooden Sword')).toHaveClass('stars-1');
    expect(slot('Hat, empty')).toHaveTextContent('Hat');
  });

  it('shows each stat with the equipment’s part beside it, and none where it adds nothing', () => {
    setup();
    expect(stat('Primary stats', 'STR')).toHaveTextContent('13 (+1)');
    expect(stat('Primary stats', 'DEX')).toHaveTextContent(/^5$/);
    expect(stat('Derived stats', 'Attack')).toHaveTextContent('31.5 (+11.5)');
    expect(stat('Derived stats', 'Defense')).toHaveTextContent(/^3$/);
    expect(stat('Derived stats', 'Evasion')).toHaveTextContent(/^8.75$/);
    expect(stat('Derived stats', 'Crit rate')).toHaveTextContent('6.5% (+1%)');
    expect(stat('Derived stats', 'Crit damage')).toHaveTextContent(/^150%$/);
  });

  it('every number follows the server while open: new stats, damage, a level-up', () => {
    setup();
    act(() => {
      character.getState().applyStatsChanged(
        create(StatsChangedSchema, {
          sheet: {
            ...sheet,
            derived: { ...sheet.derived, physicalDefense: 7, maxHp: 234 },
            derivedWorn: { physicalDefense: 4, maxHp: 30 },
          },
          hp: 180,
          maxHp: 234,
          mp: 28,
          maxMp: 28,
        }),
      );
    });
    expect(stat('Derived stats', 'Defense')).toHaveTextContent('7 (+4)');
    expect(screen.getByRole('dialog', { name: 'Character' })).toHaveTextContent('HP180 / 234');
    act(() => {
      character.getState().setVitals(150, 28);
    });
    expect(screen.getByRole('dialog', { name: 'Character' })).toHaveTextContent('HP150 / 234');
    act(() => {
      character
        .getState()
        .applyLevelUp(create(LevelUpSchema, { character: { ...ayla, level: 2, exp: 0n } }));
    });
    expect(screen.getByRole('dialog', { name: 'Character' })).toHaveTextContent('Warrior · Lv 2');
    expect(screen.getByRole('dialog', { name: 'Character' })).toHaveTextContent('EXP0 / 65');
  });

  it('a double-click on a worn item unequips it, which waits as pending; an empty slot sends nothing', async () => {
    const { calls } = setup([ok]);
    await userEvent.dblClick(slot('Hat, empty'));
    expect(calls).toHaveLength(0);
    await userEvent.dblClick(slot('Weapon: Wooden Sword'));
    expect(withoutKeys(calls)).toEqual([{ t: 'unequip', d: { equipSlot: 'weapon' } }]);
  });

  it('an inventory item dropped on a slot equips it into that slot', () => {
    const { calls } = setup([ok]);
    const carried = dataTransfer();
    carried.setData(
      'application/x-finga-item',
      JSON.stringify({ from: 'inventory', itemId: 'i-cap' }),
    );
    fireEvent.drop(slot('Hat, empty'), { dataTransfer: carried });
    expect(withoutKeys(calls)).toEqual([{ t: 'equip', d: { itemId: 'i-cap', equipSlot: 'hat' } }]);
    expect(slot('Hat, empty')).toHaveAttribute('aria-busy', 'true');
    fireEvent.drop(slot('Top, empty'), { dataTransfer: dataTransfer() }); // carries nothing of ours
    expect(calls).toHaveLength(1);
  });

  it('a worn item dragged out carries its equip slot, for the inventory to unequip it', () => {
    setup();
    const carried = dataTransfer();
    fireEvent.dragStart(slot('Weapon: Wooden Sword'), { dataTransfer: carried });
    expect(JSON.parse(carried.getData('application/x-finga-item'))).toEqual({
      from: 'worn',
      equipSlot: 'weapon',
    });
  });

  it('a refusal shows the server’s reason, and nothing moves', async () => {
    setup([new RequestError('inventory_full', 'your inventory is full')]);
    await userEvent.dblClick(slot('Weapon: Wooden Sword'));
    expect(await screen.findByRole('alert')).toHaveTextContent('your inventory is full');
    expect(slot('Weapon: Wooden Sword')).toHaveAttribute('aria-busy', 'false');
  });

  it('a timed-out unequip is retried with the same idempotency key', async () => {
    const { calls } = setup([new RequestError('timeout', 'no response'), ok]);
    await userEvent.dblClick(slot('Weapon: Wooden Sword'));
    await userEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    expect(calls).toHaveLength(2);
    expect(calls[1]!.d.idempotencyKey).toBe(calls[0]!.d.idempotencyKey);
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull();
  });

  it('hovering a worn item shows its card alone, which a drag takes away', async () => {
    setup();
    await userEvent.hover(slot('Weapon: Wooden Sword'));
    expect(screen.getByRole('group', { name: 'Wooden Sword' })).toHaveTextContent(
      'Attack 10 (10–12)',
    );
    expect(screen.queryByRole('group', { name: /^Worn/ })).toBeNull();
    fireEvent.dragStart(slot('Weapon: Wooden Sword'));
    expect(screen.queryByRole('group', { name: 'Wooden Sword' })).toBeNull();
    await userEvent.hover(slot('Hat, empty'));
    expect(screen.queryByRole('group', { name: /Leather|Wooden/ })).toBeNull();
  });

  it('a hovered worn item taken off takes its card along, and one put on later shows none until hovered', async () => {
    setup();
    await userEvent.hover(slot('Weapon: Wooden Sword'));
    const unequipped = (revision: bigint, item?: typeof sword) =>
      act(() =>
        inventory.getState().applyInventoryChanged(
          create(InventoryChangedSchema, {
            revision,
            equipment: [{ equipSlot: 'weapon', ...(item && { item }) }],
          }),
        ),
      );
    unequipped(2n);
    expect(screen.queryByRole('group', { name: 'Wooden Sword' })).toBeNull();
    await userEvent.unhover(slot('Weapon, empty'));
    unequipped(3n, sword);
    expect(screen.queryByRole('group', { name: 'Wooden Sword' })).toBeNull();
  });

  it('closes with its × button', async () => {
    const { onClose } = setup();
    await userEvent.click(screen.getByRole('button', { name: 'Close Character' }));
    expect(onClose).toHaveBeenCalled();
  });
});
