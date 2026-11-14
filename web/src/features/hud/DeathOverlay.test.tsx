import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ExpGainedSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { resetStores } from '@/testing/reset-stores';

import { DeathOverlay } from './DeathOverlay';
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
};
const welcome = create(WelcomeSchema, { token: 'tok', character: ayla, mapId: 'town', tick: 1 });
const gained = (amount: bigint, exp: bigint) => create(ExpGainedSchema, { amount, exp });

describe('DeathOverlay', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows while dead and goes on respawn', () => {
    render(<DeathOverlay />);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    act(() => character.getState().setDead(true));
    expect(screen.getByRole('alert')).toHaveTextContent('You died.');
    act(() => character.getState().setDead(false));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});

describe('Hud EXP changes', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
    character.getState().applyWelcome(welcome);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('a negative ExpGained lowers the EXP bar and shows a loss, which fades after two seconds', () => {
    render(<Hud />);
    act(() => character.getState().applyExpGained(gained(-1n, 9n)));
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuenow', '9');
    const loss = screen.getByRole('status');
    expect(loss).toHaveTextContent('−1 EXP');
    expect(loss).toHaveClass('exp-loss');
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('a gain shows as a gain, and a second gain shows again', () => {
    render(<Hud />);
    act(() => character.getState().applyExpGained(gained(5n, 15n)));
    expect(screen.getByRole('status')).toHaveTextContent('+5 EXP');
    expect(screen.getByRole('status')).toHaveClass('exp-gain');
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    act(() => character.getState().applyExpGained(gained(3n, 18n)));
    expect(screen.getByRole('status')).toHaveTextContent('+3 EXP');
  });
});
