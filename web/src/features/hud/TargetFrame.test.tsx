import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { FieldBossSchema } from '@/protocol/messages';
import { fieldBoss } from '@/state/field-boss';
import { target } from '@/state/target';
import { resetStores } from '@/testing/reset-stores';

import { TopBars } from './TargetFrame';

describe('TargetFrame', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows the locked mob’s name, level and HP over its kind’s, its bar falling as it is hit, and hides with no lock', () => {
    render(<TopBars />);
    expect(screen.queryByLabelText('Target')).not.toBeInTheDocument();
    act(() => target.getState().show({ mobId: 3, type: 'green_slime', hp: 34 }));
    const frame = screen.getByLabelText('Target');
    expect(frame).toHaveTextContent('Lv. 1');
    expect(within(frame).queryByText('Elite')).not.toBeInTheDocument();
    const hp = within(frame).getByRole('meter', { name: 'Green Slime' });
    expect([hp.getAttribute('aria-valuenow'), hp.getAttribute('aria-valuemax')]).toEqual([
      '34',
      '34',
    ]);
    act(() => target.getState().show({ mobId: 3, type: 'green_slime', hp: 20 }));
    expect(hp).toHaveAttribute('aria-valuenow', '20');
    act(() => target.getState().show(null));
    expect(screen.queryByLabelText('Target')).not.toBeInTheDocument();
  });

  it('tags an elite', () => {
    render(<TopBars />);
    act(() => target.getState().show({ mobId: 5, type: 'mossback_snail', hp: 100 }));
    expect(within(screen.getByLabelText('Target')).getByText('Elite')).toBeInTheDocument();
  });

  it('hides for the field boss, whose bar shows it, and sits under the boss bar for another mob', () => {
    render(<TopBars />);
    act(() => {
      fieldBoss
        .getState()
        .applyFieldBoss(
          create(FieldBossSchema, { mobId: 9, type: 'king_slime', leavesInMs: 600000n }),
        );
      fieldBoss.getState().setHp({ hp: 3100, maxHp: 4260 });
      target.getState().show({ mobId: 9, type: 'king_slime', hp: 3100 });
    });
    expect(screen.getByLabelText('Field boss')).toBeInTheDocument();
    expect(screen.queryByLabelText('Target')).not.toBeInTheDocument();
    act(() => target.getState().show({ mobId: 4, type: 'green_slime', hp: 34 }));
    const bars = screen.getByLabelText('Field boss').parentElement!;
    expect([bars.className, [...bars.children].map((e) => e.getAttribute('aria-label'))]).toEqual([
      'top-bars',
      ['Field boss', 'Target'],
    ]);
  });
});
