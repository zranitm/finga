import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FieldBossGoneSchema, FieldBossSchema } from '@/protocol/messages';
import { fieldBoss } from '@/state/field-boss';
import { resetStores } from '@/testing/reset-stores';

import { BossBar } from './BossBar';

describe('BossBar', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the boss while it is up and drawn: its name and its HP, with no time, as it never leaves', () => {
    render(<BossBar />);
    act(() => {
      fieldBoss
        .getState()
        .applyFieldBoss(create(FieldBossSchema, { mobId: 9, type: 'king_slime' }));
    });
    expect(screen.queryByLabelText('Field boss')).not.toBeInTheDocument(); // not drawn yet
    act(() => {
      fieldBoss.getState().setHp({ hp: 3100, maxHp: 4260 });
    });
    const bar = screen.getByRole('group', { name: 'Field boss' });
    expect([
      bar.classList.contains('side-enemy'),
      bar.querySelector('.side-mark path') !== null,
    ]).toEqual([true, true]);
    const hp = screen.getByRole('meter', { name: 'King Slime' });
    expect(hp).toHaveAttribute('aria-valuenow', '3100');
    expect(hp).toHaveAttribute('aria-valuemax', '4260');
    expect(bar.querySelector('[title="Time left"]')).toBeNull();
    act(() => {
      fieldBoss
        .getState()
        .applyGone(
          create(FieldBossGoneSchema, { mobId: 9, type: 'king_slime', backInMs: 300000n }),
        );
    });
    expect(screen.queryByLabelText('Field boss')).not.toBeInTheDocument();
  });
});
