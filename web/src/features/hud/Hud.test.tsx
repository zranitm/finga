import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { character } from '@/state/character';
import { connection } from '@/state/connection';
import { resetStores } from '@/testing/reset-stores';

import { ConnectionNotice } from './ConnectionNotice';
import { Hud } from './Hud';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 150,
  max_hp: 204,
  mp: 20,
  max_mp: 28,
};
const welcome = {
  token: 'tok',
  character: ayla,
  inventory: [],
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

describe('Hud', () => {
  beforeEach(() => {
    resetStores();
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

    act(() => character.getState().applyExpGained({ amount: 5, exp: 15 }));
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuenow', '15');

    act(() =>
      character.getState().applyLevelUp({
        character: { ...ayla, level: 2, exp: 0, hp: 230, max_hp: 230, mp: 30, max_mp: 30 },
      }),
    );
    expect(screen.getByText('Lv. 2')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '230');
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuemax', '65');

    act(() => character.getState().setVitals(100, 30));
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '100');
  });

  it('at the level cap the EXP bar is full and says MAX', () => {
    character.getState().applyWelcome({ ...welcome, character: { ...ayla, level: 30, exp: 0 } });
    render(<Hud />);
    const exp = screen.getByRole('meter', { name: 'EXP' });
    expect(exp).toHaveAttribute('aria-valuenow', '1');
    expect(exp).toHaveAttribute('aria-valuemax', '1');
    expect(exp).toHaveTextContent('MAX');
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
});
