import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
import { FieldBossSchema } from '@/protocol/messages';
import { fieldBoss } from '@/state/field-boss';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';
import { resetStores } from '@/testing/reset-stores';

import { TopBars } from './TargetFrame';

describe('TargetFrame', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows the locked mob’s statuses under its HP', () => {
    render(<TopBars />);
    const now = performance.now();
    act(() => {
      target.getState().show({ kind: 'mob', ref: 'm:3', type: 'green_slime', hp: 34 });
      statuses.getState().show({
        me: [],
        target: [
          { id: 'slow', from: now, until: now + 2000 },
          { id: 'stun', from: now, until: now + 1000 },
        ],
        party: {},
      });
    });
    const frame = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
    const list = within(frame).getByRole('list', { name: 'Statuses' });
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((li) => li.title),
    ).toEqual(['Stun', 'Slow']);
  });

  it('shows the locked mob’s name, level and HP over its kind’s, its bar falling as it is hit, and hides with no lock', () => {
    render(<TopBars />);
    expect(screen.queryByLabelText(/^Target/)).not.toBeInTheDocument();
    act(() => target.getState().show({ kind: 'mob', ref: 'm:3', type: 'green_slime', hp: 34 }));
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
    act(() => target.getState().show({ kind: 'mob', ref: 'm:3', type: 'green_slime', hp: 20 }));
    expect(hp).toHaveAttribute('aria-valuenow', '20');
    act(() => target.getState().show(null));
    expect(screen.queryByLabelText(/^Target/)).not.toBeInTheDocument();
  });

  it('tags an elite', () => {
    render(<TopBars />);
    act(() => target.getState().show({ kind: 'mob', ref: 'm:5', type: 'mossback_snail', hp: 100 }));
    expect(
      within(screen.getByLabelText('Target: Mossback Snail, enemy')).getByText('Elite'),
    ).toBeInTheDocument();
  });

  it('shows a locked enemy player’s name, level, class and HP, with a BOT tag on a bot and no mob stats', () => {
    render(<TopBars />);
    const robin = {
      kind: 'player',
      ref: 'p:c-robin',
      name: 'Robin',
      cls: 'thief',
      level: 30,
      hp: 210,
      maxHp: 400,
      bot: false,
    } as const;
    act(() => target.getState().show(robin));
    const frame = screen.getByRole('group', { name: 'Target: Robin, enemy' });
    expect(frame).toHaveTextContent('Lv. 30');
    expect(frame).toHaveTextContent('Thief');
    expect(within(frame).queryByText('BOT')).not.toBeInTheDocument();
    expect(within(frame).queryByRole('list', { name: /stats/ })).not.toBeInTheDocument();
    const hp = within(frame).getByRole('meter', { name: 'Robin HP' });
    expect([hp.getAttribute('aria-valuenow'), hp.getAttribute('aria-valuemax')]).toEqual([
      '210',
      '400',
    ]);
    act(() => target.getState().show({ ...robin, bot: true }));
    expect(within(frame).getByText('BOT')).toBeInTheDocument();
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
      target.getState().show({ kind: 'mob', ref: 'm:9', type: 'king_slime', hp: 3100 });
    });
    const bars = screen.getByLabelText('Field boss').parentElement!;
    expect([bars.className, [...bars.children].map((e) => e.getAttribute('aria-label'))]).toEqual([
      'top-bars',
      ['Field boss', 'Target: King Slime, enemy'],
    ]);
    act(() => target.getState().show({ kind: 'mob', ref: 'm:4', type: 'green_slime', hp: 34 }));
    expect([...bars.children].map((e) => e.getAttribute('aria-label'))).toEqual([
      'Field boss',
      'Target: Green Slime, enemy',
    ]);
  });

  it('lists the mob’s stats under its HP, each short with the Character window’s full name', () => {
    render(<TopBars />);
    act(() => target.getState().show({ kind: 'mob', ref: 'm:3', type: 'green_slime', hp: 34 }));
    const frame = screen.getByRole('group', { name: 'Target: Green Slime, enemy' });
    const stats = within(frame).getByRole('list', { name: 'Green Slime stats' });
    expect(within(frame).getByRole('meter').nextElementSibling).toBe(stats);
    const rows = within(stats).getAllByRole('listitem');
    const shown = (r: HTMLElement) => [
      r.querySelector('.unit-frame-stat')!.textContent,
      r.lastChild!.textContent,
    ];
    expect(rows.map((r) => [...shown(r), r.title, r.getAttribute('aria-label')])).toEqual([
      ['ATK', '6', 'Physical attack', 'Physical attack 6'],
      ['DEF', '2', 'Physical defense', 'Physical defense 2'],
      ['M.DEF', '1', 'Magic defense', 'Magic defense 1'],
      ['ACC', '24', 'Accuracy', 'Accuracy 24'],
      ['EVA', '2', 'Evasion', 'Evasion 2'],
    ]);
  });

  it('names a magic mob’s ATK Magic attack, its short name in magic’s colour, and a physical one’s in physical’s, as their skills wear them', () => {
    const magic = mobs.find((m) => m.damage === 'magic')!;
    render(<TopBars />);
    act(() => target.getState().show({ kind: 'mob', ref: 'm:3', type: magic.id, hp: magic.hp }));
    const colorOf = (name: string) =>
      screen.getByRole('listitem', { name }).querySelector<HTMLElement>('.unit-frame-stat')!.style
        .color;
    const atk = screen.getByRole('listitem', { name: `Magic attack ${magic.attack}` });
    expect([atk.title, colorOf(`Magic attack ${magic.attack}`)]).toEqual([
      'Magic attack',
      'rgb(176, 112, 240)',
    ]);
    act(() => target.getState().show({ kind: 'mob', ref: 'm:4', type: 'green_slime', hp: 34 }));
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
        target.getState().show({ kind: 'mob', ref: 'm:4', type: 'green_slime', hp: 34 });
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

    // In Chrome these rules put every mob's five on one line in the window's 219 px, the Shade Lord's widest at 216; five 3-digit values need 246 and wrap.
    it('sets the Crypt Warden’s five to share one line: short names at 10 px, 2 px from their 11 px numbers, the stats 5 px apart', () => {
      render(<TopBars />);
      act(() =>
        target.getState().show({ kind: 'mob', ref: 'm:4', type: 'crypt_warden', hp: 9000 }),
      );
      const list = screen.getByRole('list', { name: 'Crypt Warden stats' });
      expect(list.textContent).toBe('ATK204DEF45M.DEF27ACC62EVA23'); // no spaces to break at, or to widen it
      const cs = getComputedStyle(list);
      expect(
        ['display', 'flex-wrap', 'gap', 'font-size'].map((k) => cs.getPropertyValue(k)),
      ).toEqual([
        'flex',
        'wrap', // anything longer still wraps
        '2px 5px',
        '11px',
      ]);
      for (const name of list.querySelectorAll('.unit-frame-stat')) {
        expect(getComputedStyle(name)).toMatchObject({ fontSize: '10px', marginRight: '2px' });
      }
    });
  });
});
