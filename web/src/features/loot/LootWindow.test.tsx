import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { PouchKind, PouchViewSchema } from '@/protocol/messages';
import { pouches } from '@/state/pouches';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { LootWindow } from './LootWindow';

/** Pouch id holding items, each [template, quantity], with stats on equipment. */
const pouch = (id: number, items: [string, number][], goneMs = 42000) =>
  create(PouchViewSchema, {
    id,
    kind: PouchKind.POUCH,
    items: items.map(([template, quantity], i) => ({
      item: i + 1,
      template,
      quantity,
      ...(template === 'iron_sword' && { stats: { physicalAttack: 17 } }),
    })),
    goneInMs: BigInt(goneMs),
  });

/** A socket whose TakeLoot answers each pick with the next codes, in order: 0 is taken. */
function fakeSocket(answers: (number[] | Error)[] = []) {
  const calls: { t: string; d: { picks: { pouch: number; item: number }[] } }[] = [];
  const request = vi.fn((t: string, d: { picks: { pouch: number; item: number }[] }) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? [];
    return a instanceof Error
      ? Promise.reject(a)
      : Promise.resolve({ ok: true, picks: d.picks.map((pick, i) => ({ pick, code: a[i] ?? 0 })) });
  });
  return { socket: { request } as never, calls };
}

const row = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name}`) });
const picked = (calls: ReturnType<typeof fakeSocket>['calls']) =>
  calls.map((c) => c.d.picks.map((p) => `${p.pouch}/${p.item}`));

describe('LootWindow', () => {
  beforeEach(() => {
    resetStores();
    pouches.getState().applySnapshot([
      pouch(1, [
        ['slime_gel', 2],
        ['iron_sword', 1],
      ]),
      pouch(2, [['red_potion', 1]]),
      pouch(3, [['gold_ring', 1]]),
    ]);
    pouches.getState().setInReach([1, 2]);
  });

  it('lists every item of the pouches in reach as one list, in its star colour with its quantity, and how soon the first goes', () => {
    render(<LootWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    const rows = within(screen.getByRole('list', { name: 'Items in reach' })).getAllByRole(
      'listitem',
    );
    expect(rows.map((r) => r.textContent)).toEqual([
      'SGSlime Gel ★×2',
      'ISIron Sword ★★',
      'RPRed Potion ★',
    ]);
    expect(rows.map((r) => r.className)).toEqual(['stars-1', 'stars-2', 'stars-1']);
    expect(screen.getByText('Gone in 42 s')).toBeInTheDocument();
  });

  it('hovering a row shows its item’s card with what it rolled', async () => {
    render(<LootWindow socket={fakeSocket().socket} onClose={vi.fn()} />);
    await user.hover(row('Iron Sword'));
    expect(screen.getByRole('group', { name: 'Iron Sword' })).toHaveTextContent(
      'Physical attack 17',
    );
  });

  it('a click takes its row, which waits for the answer and can’t be clicked twice', async () => {
    const { socket, calls } = fakeSocket([[0]]);
    render(<LootWindow socket={socket} onClose={vi.fn()} />);
    await user.click(row('Iron Sword'));
    expect(calls.map((c) => c.t)).toEqual(['takeLoot']);
    expect(picked(calls)).toEqual([['1/2']]);
    expect(row('Iron Sword')).toBeDisabled();
    await user.click(row('Iron Sword'));
    expect(calls).toHaveLength(1);
    act(() => pouches.getState().applyDelta([pouch(1, [['slime_gel', 2]])], [])); // the sword left its pouch
    expect(screen.queryByRole('button', { name: /^Iron Sword/ })).toBeNull();
  });

  it('Take all takes every row not already being taken in one TakeLoot, and a refused row stays with why', async () => {
    const { socket, calls } = fakeSocket([[0], [ERR.ETC_TAB_FULL, 0]]);
    render(<LootWindow socket={socket} onClose={vi.fn()} />);
    await user.click(row('Red Potion'));
    await user.click(screen.getByRole('button', { name: 'Take all' }));
    expect(picked(calls)).toEqual([['2/1'], ['1/1', '1/2']]);
    const gel = row('Slime Gel').closest('li')!;
    expect(within(gel).getByRole('alert')).toHaveTextContent('Your Etc tab is full.');
    expect(row('Slime Gel')).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Take all' })).toBeEnabled();
    act(() => pouches.getState().applyDelta([pouch(1, [['slime_gel', 2]])], [2])); // the sword and the potion left
    expect(screen.getByRole('button', { name: 'Take all' })).toBeEnabled(); // the gel, to try again
  });

  it('a take the server refuses whole refuses each of its rows', async () => {
    const { socket } = fakeSocket([refusal(ERR.CHARACTER_DEAD)]);
    render(<LootWindow socket={socket} onClose={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Take all' }));
    expect(screen.getAllByRole('alert').map((a) => a.textContent)).toEqual(
      Array(3).fill('You can’t do that while dead.'),
    );
  });

  it('Take all waits while every row is being taken', async () => {
    const request = vi.fn(() => new Promise(() => undefined)); // no answer yet
    render(<LootWindow socket={{ request } as never} onClose={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Take all' }));
    expect(screen.getByRole('button', { name: 'Take all' })).toBeDisabled();
  });
});
