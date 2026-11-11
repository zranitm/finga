import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  StatsChangedSchema,
  ExpGainedSchema,
  LevelUpSchema,
  LootKind,
  LootSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';

import { ConnectionNotice } from './ConnectionNotice';
import { Hud } from './Hud';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 150,
  maxHp: 204,
  mp: 20,
  maxMp: 28,
};
const welcome = create(WelcomeSchema, { token: 'tok', character: ayla, mapId: 'town', tick: 1 });
const gained = (amount: bigint, exp: bigint) => create(ExpGainedSchema, { amount, exp });

describe('Hud', () => {
  beforeEach(() => {
    resetStores();
  });

  it('a badge counts the unspent stat points and opens the character window', async () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    expect(screen.queryByRole('button', { name: /stat point/ })).toBeNull();
    const points = (unspent: number) =>
      act(() =>
        character.getState().applyStatsChanged(
          create(StatsChangedSchema, {
            sheet: { points: { unspent } },
            hp: 150,
            maxHp: 204,
            mp: 20,
            maxMp: 28,
          }),
        ),
      );
    points(1);
    expect(screen.getByRole('button', { name: '1 stat point' })).toBeInTheDocument();
    points(5);
    await userEvent.click(screen.getByRole('button', { name: '5 stat points' }));
    expect(ui.getState().windows).toContain('character');
    points(0);
    expect(screen.queryByRole('button', { name: /stat point/ })).toBeNull();
  });

  it('shows nothing before a character arrives', () => {
    render(<Hud />);
    expect(screen.queryByLabelText('Status')).not.toBeInTheDocument();
  });

  it('renders the bars from the store and updates on level up and EXP gained', () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    expect(screen.getByText('Ayla')).toBeInTheDocument();
    expect(screen.getByText('Lv. 1')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '150');
    expect(screen.getByRole('meter', { name: 'MP' })).toHaveAttribute('aria-valuenow', '20');
    const exp = screen.getByRole('meter', { name: 'EXP' });
    expect(exp).toHaveAttribute('aria-valuenow', '10');
    expect(exp).toHaveAttribute('aria-valuemax', '20');

    act(() => character.getState().applyExpGained(gained(5n, 15n)));
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuenow', '15');

    act(() =>
      character.getState().applyLevelUp(
        create(LevelUpSchema, {
          character: { ...ayla, level: 2, exp: 0n, hp: 230, maxHp: 230, mp: 30, maxMp: 30 },
        }),
      ),
    );
    expect(screen.getByText('Lv. 2')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '230');
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuemax', '65');

    act(() => character.getState().setVitals(100, 30));
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '100');
  });

  it('at the level cap the EXP bar is full and says MAX', () => {
    character
      .getState()
      .applyWelcome(create(WelcomeSchema, { character: { ...ayla, level: 30, exp: 0n } }));
    render(<Hud />);
    const exp = screen.getByRole('meter', { name: 'EXP' });
    expect(exp).toHaveAttribute('aria-valuenow', '1');
    expect(exp).toHaveAttribute('aria-valuemax', '1');
    expect(exp).toHaveTextContent('MAX');
  });

  it('says how many drops wait for room while any do', () => {
    const loot = (kind: LootKind, dropId: number) =>
      create(LootSchema, {
        kind,
        mapId: 'meadow',
        dropId,
        ownerId: 'c-ayla',
        template: 'slime_gel',
      });
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    expect(screen.queryByText(/Inventory full/)).not.toBeInTheDocument();
    act(() => {
      combatLog.getState().applyLoot(loot(LootKind.WAITING, 7));
      combatLog.getState().applyLoot(loot(LootKind.WAITING, 8));
    });
    expect(screen.getByText('Inventory full: 2 waiting')).toBeInTheDocument();
    act(() => combatLog.getState().applyLoot(loot(LootKind.COLLECTED, 7)));
    expect(screen.getByText('Inventory full: 1 waiting')).toBeInTheDocument();
    act(() => combatLog.getState().applyLoot(loot(LootKind.LOST, 8)));
    expect(screen.queryByText(/Inventory full/)).not.toBeInTheDocument();
  });
});

describe('ConnectionNotice', () => {
  beforeEach(() => {
    resetStores();
  });

  it.each([
    {
      name: 'disconnected',
      set: () => connection.getState().setConnection('disconnected'),
      shown: false,
    },
    {
      name: 'connecting',
      set: () => connection.getState().setConnection('connecting'),
      shown: false,
    },
    {
      name: 'connected',
      set: () => connection.getState().setConnection('connected'),
      shown: false,
    },
    {
      name: 'reconnecting',
      set: () => connection.getState().setConnection('reconnecting', { attempt: 1 }),
      shown: true,
    },
  ])('$name shows the notice: $shown', ({ set, shown }) => {
    set();
    render(<ConnectionNotice />);
    expect(screen.queryByRole('status') !== null).toBe(shown);
  });

  it('shows on reconnecting with the attempt, and hides on connected', () => {
    render(<ConnectionNotice />);
    act(() => connection.getState().setConnection('reconnecting', { attempt: 1 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting…');
    act(() => connection.getState().setConnection('reconnecting', { attempt: 3 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting (attempt 3)…');
    act(() => connection.getState().setConnection('connected'));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('a stalled world on an open socket says it waits for the server; a reconnect says so instead', () => {
    act(() => connection.getState().setConnection('connected'));
    render(<ConnectionNotice />);
    act(() => connection.getState().setWorldStalled(true));
    expect(screen.getByRole('status')).toHaveTextContent('Waiting for the server…');
    act(() => connection.getState().setConnection('reconnecting', { attempt: 1 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting…');
    act(() => connection.getState().setConnection('connected'));
    act(() => connection.getState().setWorldStalled(false));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
