import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
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
    expect(screen.queryByLabelText(/^Target/)).not.toBeInTheDocument();
    act(() => target.getState().show({ mobId: 3, type: 'green_slime', hp: 34 }));
    const frame = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
    expect(frame).toHaveTextContent('Lv. 1');
    expect(frame).toHaveClass('unit-frame', 'side-enemy');
    expect(frame.querySelector('.side-mark path')).not.toBeNull(); // the enemy's cross
    expect(within(frame).queryByText('Elite')).not.toBeInTheDocument();
    const hp = within(frame).getByRole('meter', { name: 'Green Slime HP' });
    expect([hp.getAttribute('aria-valuenow'), hp.getAttribute('aria-valuemax')]).toEqual([
      '34',
      '34',
    ]);
    act(() => target.getState().show({ mobId: 3, type: 'green_slime', hp: 20 }));
    expect(hp).toHaveAttribute('aria-valuenow', '20');
    act(() => target.getState().show(null));
    expect(screen.queryByLabelText(/^Target/)).not.toBeInTheDocument();
  });

  it('tags an elite', () => {
    render(<TopBars />);
    act(() => target.getState().show({ mobId: 5, type: 'mossback_snail', hp: 100 }));
    expect(
      within(screen.getByLabelText('Target: Mossback Snail, enemy')).getByText('Elite'),
    ).toBeInTheDocument();
  });

  it('shows for the field boss too, under its bar, as for any other mob', () => {
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
    const bars = screen.getByLabelText('Field boss').parentElement!;
    expect([bars.className, [...bars.children].map((e) => e.getAttribute('aria-label'))]).toEqual([
      'top-bars',
      ['Field boss', 'Target: King Slime, enemy'],
    ]);
    act(() => target.getState().show({ mobId: 4, type: 'green_slime', hp: 34 }));
    expect([...bars.children].map((e) => e.getAttribute('aria-label'))).toEqual([
      'Field boss',
      'Target: Green Slime, enemy',
    ]);
  });

  it('lists the mob’s stats under its HP, each short with the Character window’s full name', () => {
    render(<TopBars />);
    act(() => target.getState().show({ mobId: 3, type: 'green_slime', hp: 34 }));
    const frame = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
    const stats = within(frame).getByRole('list', { name: 'Green Slime stats' });
    expect(within(frame).getByRole('meter').nextElementSibling).toBe(stats);
    const rows = within(stats).getAllByRole('listitem');
    expect(rows.map((r) => [r.textContent, r.title, r.getAttribute('aria-label')])).toEqual([
      ['ATK 6', 'Physical attack', 'Physical attack 6'],
      ['DEF 2', 'Physical defense', 'Physical defense 2'],
      ['M.DEF 1', 'Magic defense', 'Magic defense 1'],
      ['ACC 24', 'Accuracy', 'Accuracy 24'],
      ['EVA 2', 'Evasion', 'Evasion 2'],
    ]);
  });

  it('names a magic mob’s ATK Magic attack, its short name in magic’s colour, and a physical one’s in physical’s, as their skills wear them', () => {
    const magic = mobs.find((m) => m.damage === 'magic')!;
    render(<TopBars />);
    act(() => target.getState().show({ mobId: 3, type: magic.id, hp: magic.hp }));
    const colorOf = (name: string) =>
      screen.getByRole('listitem', { name }).querySelector<HTMLElement>('.unit-frame-stat')!.style
        .color;
    const atk = screen.getByRole('listitem', { name: `Magic attack ${magic.attack}` });
    expect([atk.title, colorOf(`Magic attack ${magic.attack}`)]).toEqual([
      'Magic attack',
      'rgb(176, 112, 240)',
    ]);
    act(() => target.getState().show({ mobId: 4, type: 'green_slime', hp: 34 }));
    expect([colorOf('Physical attack 6'), colorOf('Physical defense 2')]).toEqual([
      'rgb(255, 106, 61)',
      '', // the others' names muted, by the stylesheet
    ]);
  });

  describe('drawn by the stylesheet', () => {
    let style: HTMLStyleElement;
    beforeEach(() => {
      style = document.createElement('style');
      style.textContent = readFileSync(
        path.resolve(process.cwd(), 'src/styles/global.css'),
        'utf8',
      );
      document.head.append(style);
    });
    afterEach(() => {
      style.remove();
    });

    // jsdom lays nothing out, so these check the rules the browser draws by.
    it('is compact and 240 px wide, centred under the boss bar’s 420, with a thin HP bar and its numbers over it', () => {
      render(<TopBars />);
      act(() => {
        fieldBoss
          .getState()
          .applyFieldBoss(
            create(FieldBossSchema, { mobId: 9, type: 'king_slime', leavesInMs: 600000n }),
          );
        fieldBoss.getState().setHp({ hp: 3100, maxHp: 4260 });
        target.getState().show({ mobId: 4, type: 'green_slime', hp: 34 });
      });
      const frame = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
      expect(frame).toHaveClass('unit-frame-compact');
      expect(getComputedStyle(frame)).toMatchObject({
        width: '240px',
        alignSelf: 'center',
        padding: '5px 8px',
        fontSize: '12px',
      });
      expect(getComputedStyle(frame.parentElement!).width).toContain('420px');
      expect(getComputedStyle(screen.getByLabelText('Field boss')).width).not.toBe('240px');
      const bar = within(frame).getByRole('meter');
      expect(getComputedStyle(bar).height).toBe('8px');
      expect(getComputedStyle(bar.querySelector('.bar-text')!).fontSize).toBe('11px');
      expect(getComputedStyle(within(frame).getByRole('list')).fontSize).toBe('11px');
    });
  });
});
