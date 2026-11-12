import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FieldBossGoneSchema, FieldBossSchema } from '@/protocol/messages';
import { fieldBoss } from '@/state/field-boss';
import { resetStores } from '@/testing/reset-stores';

import { BossBar, timeLeft } from './BossBar';

describe('BossBar', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the boss while it is up and drawn: its name, its HP and the time it has left, counting down', () => {
    render(<BossBar />);
    act(() => {
      fieldBoss
        .getState()
        .applyFieldBoss(
          create(FieldBossSchema, { mobId: 9, type: 'king_slime', leavesInMs: 600000n }),
        );
    });
    expect(screen.queryByLabelText('Field boss')).not.toBeInTheDocument(); // not drawn yet
    act(() => {
      fieldBoss.getState().setHp({ hp: 3100, maxHp: 4260 });
    });
    const hp = screen.getByRole('meter', { name: 'King Slime' });
    expect(hp).toHaveAttribute('aria-valuenow', '3100');
    expect(hp).toHaveAttribute('aria-valuemax', '4260');
    expect(screen.getByTitle('Time left')).toHaveTextContent('10:00');
    act(() => {
      vi.advanceTimersByTime(61000);
    });
    expect(screen.getByTitle('Time left')).toHaveTextContent('8:59');
    act(() => {
      fieldBoss.getState().applyGone(create(FieldBossGoneSchema, { mobId: 9, beaten: true }));
    });
    expect(screen.queryByLabelText('Field boss')).not.toBeInTheDocument();
  });

  it.each([
    { ms: 600000, text: '10:00' },
    { ms: 59001, text: '1:00' },
    { ms: 9000, text: '0:09' },
    { ms: -500, text: '0:00' },
  ])('$ms ms left reads $text', ({ ms, text }) => {
    expect(timeLeft(ms)).toBe(text);
  });
});
