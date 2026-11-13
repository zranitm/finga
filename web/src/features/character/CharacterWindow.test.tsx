import { create } from '@bufbuild/protobuf';
import type { MessageInitShape } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import {
  InventoryChangedSchema,
  LevelUpSchema,
  ResponseSchema,
  StatsChangedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import type { Data, StatSheet, StatSheetSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';
import { skillBar } from '@/state/skill-bar';
import { dataTransfer } from '@/testing/data-transfer';
import { refusal } from '@/testing/refusal';
import { withoutKeys } from '@/testing/requests';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

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

/** A StatsChanged with the sheet changed as given, the vitals as they were. */
function stats(change: Exclude<MessageInitShape<typeof StatSheetSchema>, StatSheet>) {
  character.getState().applyStatsChanged(
    create(StatsChangedSchema, {
      sheet: { ...sheet, ...change },
      hp: 180,
      maxHp: 204,
      mp: 28,
      maxMp: 28,
    }),
  );
}

const slot = (name: string) => screen.getByRole('button', { name });
const plus = (stat: string) => screen.getByRole('button', { name: `Add a point to ${stat}` });
const button = (name: string) =>
  within(screen.getByRole('group', { name: 'Stat points' })).getByRole('button', { name });
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
    expect(stat('Offense', 'Physical attack')).toHaveTextContent('31.5 (+11.5)');
    expect(stat('Defense', 'Physical defense')).toHaveTextContent(/^3$/);
    expect(stat('Defense', 'Evasion')).toHaveTextContent(/^8.75$/);
    expect(stat('Offense', 'Crit rate')).toHaveTextContent('6.5% (+1)'); // a share leaves the unit to the total
    expect(stat('Offense', 'Crit damage')).toHaveTextContent(/^150%$/);
  });

  it('shows the skills’ part beside the equipment’s, and a preview keeps the passives', async () => {
    setup();
    // A Warrior learns the Beginner's skills: 12 points in Second Wind, Deep Breath, add 2.2 HP a second to 0.5.
    act(() =>
      stats({
        derived: { ...sheet.derived, hpRegen: 2.7 },
        derivedSkills: { hpRegen: 2.2 },
        skills: { unspent: 0, invested: { second_wind: 12 } },
        points: { unspent: 5, spent: {}, freeResetLeft: true },
      }),
    );
    expect(stat('Defense', 'HP regen')).toHaveTextContent(/^2\.7\/s \(\+2\.2\)$/);
    expect(screen.getByTitle('From skills')).toHaveTextContent('+2.2');
    await user.click(plus('STR'));
    expect(stat('Offense', 'Physical attack')).toHaveTextContent('→');
    expect(stat('Defense', 'HP regen')).not.toHaveTextContent('→');
  });

  it('a preview keeps the buffs on, as the world counts them in the skills’ part', async () => {
    setup();
    // Second Wind's bonus as a buff at 12 points: Deep Breath's 2.2 HP a second on top of 0.5.
    skillBar.getState().buff({ skill: 'second_wind', points: 12, until: Number.POSITIVE_INFINITY });
    act(() =>
      stats({
        derived: { ...sheet.derived, hpRegen: 2.7 },
        derivedSkills: { hpRegen: 2.2 },
        points: { unspent: 5, spent: {}, freeResetLeft: true },
      }),
    );
    await user.click(plus('STR'));
    expect(stat('Offense', 'Physical attack')).toHaveTextContent('→');
    expect(stat('Defense', 'HP regen')).not.toHaveTextContent('→');
  });

  it('puts the equipment’s and the skills’ parts in one bracket, the equipment’s first', () => {
    setup();
    act(() =>
      stats({
        derived: { ...sheet.derived, hpRegen: 3.2 },
        derivedWorn: { ...sheet.derivedWorn, hpRegen: 0.5 },
        derivedSkills: { hpRegen: 2.2 },
      }),
    );
    expect(stat('Defense', 'HP regen')).toHaveTextContent(/^3\.2\/s \(\+0\.5 \+2\.2\)$/);
    const parts = within(stat('Defense', 'HP regen') as HTMLElement).getAllByTitle(/^From /);
    expect(parts.map((p) => [p.title, p.textContent])).toEqual([
      ['From equipment', '+0.5'],
      ['From skills', ' +2.2'],
    ]);
  });

  it('shows the sixteen derived stats in four groups, in their units', () => {
    setup();
    const names = (table: string) =>
      within(screen.getByRole('table', { name: table }))
        .getAllByRole('rowheader')
        .map((th) => th.getAttribute('aria-label') ?? th.textContent);
    expect(names('Offense')).toEqual([
      'Physical attack',
      'Magic attack',
      'Accuracy',
      'Crit rate',
      'Crit damage',
      'Penetration',
    ]);
    expect(names('Defense')).toEqual([
      'Max HP',
      'Physical defense',
      'Magic defense',
      'Evasion',
      'HP regen',
    ]);
    expect(names('Resources')).toEqual(['Max MP', 'MP regen']);
    expect(names('Speed')).toEqual(['Attack speed', 'Cooldown speed', 'Move speed (combat maps)']);
    act(() => {
      stats({ derived: { ...sheet.derived, hpRegen: 0.5, moveSpeed: 0.0123 } });
    });
    expect(stat('Defense', 'HP regen')).toHaveTextContent(/^0.5\/s$/);
    expect(stat('Speed', 'Move speed (combat maps)')).toHaveTextContent(/^1.23%$/);
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
    expect(stat('Defense', 'Physical defense')).toHaveTextContent('7 (+4)');
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
    await user.dblClick(slot('Hat, empty'));
    expect(calls).toHaveLength(0);
    await user.dblClick(slot('Weapon: Wooden Sword'));
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

  it('a refusal shows the client’s words for it, and nothing moves', async () => {
    setup([refusal(ERR.EQUIP_TAB_FULL)]);
    await user.dblClick(slot('Weapon: Wooden Sword'));
    expect(await screen.findByRole('alert')).toHaveTextContent('Your Equip tab is full.');
    expect(slot('Weapon: Wooden Sword')).toHaveAttribute('aria-busy', 'false');
  });

  it('a timed-out unequip is retried with the same idempotency key', async () => {
    const { calls } = setup([new RequestError('timeout', 'no response'), ok]);
    await user.dblClick(slot('Weapon: Wooden Sword'));
    await user.click(await screen.findByRole('button', { name: 'Retry' }));
    expect(calls).toHaveLength(2);
    expect(calls[1]!.d.idempotencyKey).toBe(calls[0]!.d.idempotencyKey);
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull();
  });

  it('hovering a worn item shows its card alone, which a drag takes away', async () => {
    setup();
    await user.hover(slot('Weapon: Wooden Sword'));
    expect(screen.getByRole('group', { name: 'Wooden Sword' })).toHaveTextContent(
      'Physical attack 10 (10–12)',
    );
    expect(screen.queryByRole('group', { name: /^Worn/ })).toBeNull();
    fireEvent.dragStart(slot('Weapon: Wooden Sword'), { dataTransfer: dataTransfer() });
    expect(screen.queryByRole('group', { name: 'Wooden Sword' })).toBeNull();
    await user.hover(slot('Hat, empty'));
    expect(screen.queryByRole('group', { name: /Leather|Wooden/ })).toBeNull();
  });

  it('a hovered worn item taken off takes its card along, and one put on later shows none until hovered', async () => {
    setup();
    await user.hover(slot('Weapon: Wooden Sword'));
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
    await user.unhover(slot('Weapon, empty'));
    unequipped(3n, sword);
    expect(screen.queryByRole('group', { name: 'Wooden Sword' })).toBeNull();
  });

  it('without points to spend there is no +, Apply or Auto-assign', () => {
    setup();
    expect(screen.queryByRole('button', { name: /^Add a point/ })).toBeNull();
    expect(screen.getByRole('group', { name: 'Stat points' })).toHaveTextContent(
      'No points to spend',
    );
    expect(screen.queryByRole('button', { name: 'Auto-assign' })).toBeNull();
  });

  it('+ stages points until none are left, previews what they change, and Undo returns them', async () => {
    setup();
    act(() => stats({ points: { unspent: 5, spent: {}, freeResetLeft: true } }));
    for (const s of ['STR', 'STR', 'STR', 'VIT', 'VIT']) {
      await user.click(plus(s));
    }
    expect(plus('DEX')).toBeDisabled();
    expect(screen.getByRole('group', { name: 'Stat points' })).toHaveTextContent('Points: 0');
    expect(stat('Primary stats', 'STR')).toHaveTextContent('13 (+1) +3');
    // 15 own STR and the sword's 1: 1.5 × 16 + 0.5 × 5 DEX + the sword's 10; 60 + 2 × 16 + 10 × 12 VIT.
    expect(stat('Offense', 'Physical attack')).toHaveTextContent('31.5 (+11.5) → 36.5');
    expect(stat('Defense', 'Max HP')).toHaveTextContent('204 → 212');
    expect(stat('Offense', 'Accuracy')).not.toHaveTextContent('→');
    await user.click(button('Undo'));
    expect(stat('Primary stats', 'STR')).toHaveTextContent(/^13 \(\+1\)\+$/);
    expect(stat('Offense', 'Physical attack')).not.toHaveTextContent('→');
    expect(screen.getByRole('group', { name: 'Stat points' })).toHaveTextContent('Points: 5');
  });

  it('Apply sends the staged points with the unspent count it saw, and clears them once applied', async () => {
    const { calls } = setup([ok]);
    act(() => stats({ points: { unspent: 5, spent: {}, freeResetLeft: true } }));
    await user.click(plus('DEX'));
    await user.click(plus('AGI'));
    await user.click(button('Apply'));
    expect(calls).toEqual([
      {
        t: 'spendPoints',
        d: { expectedUnspent: 5, points: { str: 0, dex: 1, int: 0, agi: 1, vit: 0 } },
      },
    ]);
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    expect(stat('Primary stats', 'DEX')).not.toHaveTextContent('+1');
  });

  it('a points_changed answer means it already landed; another refusal shows its words', async () => {
    setup([refusal(ERR.POINTS_CHANGED), refusal(ERR.NOT_ENOUGH_POINTS)]);
    act(() => stats({ points: { unspent: 5, spent: {}, freeResetLeft: true } }));
    await user.click(plus('INT'));
    await user.click(button('Apply'));
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    await user.click(plus('INT'));
    await user.click(button('Apply'));
    expect(await screen.findByRole('alert')).toHaveTextContent('You don’t have that many points.');
  });

  it('a draft is void once the unspent count changes, so it is never spent from a stale count', async () => {
    setup();
    act(() => stats({ points: { unspent: 5, spent: {}, freeResetLeft: true } }));
    await user.click(plus('STR'));
    act(() => stats({ points: { unspent: 10, spent: {}, freeResetLeft: true } }));
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    expect(screen.getByRole('group', { name: 'Stat points' })).toHaveTextContent('Points: 10');
  });

  it('Auto-assign spends every unspent point by the class’s split', async () => {
    const { calls } = setup([ok]);
    act(() => stats({ points: { unspent: 7, spent: {}, freeResetLeft: true } }));
    await user.click(button('Auto-assign'));
    expect(calls).toEqual([
      {
        t: 'spendPoints',
        d: { expectedUnspent: 7, points: { str: 5, dex: 0, int: 0, agi: 0, vit: 2 } },
      },
    ]);
  });

  it('Reset asks first, then sends the count it saw; once the free reset is used it is off', async () => {
    const { calls } = setup([ok]);
    act(() => stats({ points: { unspent: 0, spent: { str: 3, vit: 2 }, freeResetLeft: true } }));
    await user.click(button('Reset'));
    const ask = screen.getByRole('dialog', { name: 'Reset stat points' });
    expect(ask).toHaveTextContent('All 5 spent points go back to unspent');
    await user.click(within(ask).getByRole('button', { name: 'Cancel' }));
    expect(calls).toHaveLength(0);
    await user.click(button('Reset'));
    await user.click(
      within(screen.getByRole('dialog', { name: 'Reset stat points' })).getByRole('button', {
        name: 'Reset',
      }),
    );
    expect(calls).toEqual([{ t: 'resetPoints', d: { expectedUnspent: 0 } }]);
    act(() => stats({ points: { unspent: 5, spent: {}, freeResetLeft: false } }));
    expect(button('Reset')).toBeDisabled();
    expect(button('Reset')).toHaveAttribute('title', 'The free reset is used');
  });

  it('a worn item whose requirement is not met is greyed out', () => {
    act(() => {
      inventory.getState().applyInventoryChanged(
        create(InventoryChangedSchema, {
          revision: 2n,
          equipment: [
            {
              equipSlot: 'weapon',
              item: {
                itemId: 'i-bow',
                template: 'hunting_bow',
                quantity: 1,
                stats: { physicalAttack: 15 },
              },
            },
          ],
        }),
      );
    });
    setup();
    expect(slot('Weapon: Hunting Bow, inactive')).toHaveClass('slot-inactive');
  });

  it('closes with its × button', async () => {
    const { onClose } = setup();
    await user.click(screen.getByRole('button', { name: 'Close Character' }));
    expect(onClose).toHaveBeenCalled();
  });
});
