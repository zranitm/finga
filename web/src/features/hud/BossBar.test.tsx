import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { mobs } from '@/gamedata/mobs';
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

    // jsdom lays nothing out, so these check the rules the browser draws by, and the room the longest name and numbers need in them by a generous glyph width.
    it('is a compact frame in the 180 px column: the mark and the full name on a row, the HP bar under it, the numbers on the bar, the boss’s accent border kept', () => {
      const bosses = mobs.filter((m) => m.boss);
      const longest = bosses.reduce((a, b) => (b.name.length > a.name.length ? b : a));
      const widest = Math.max(...bosses.map((m) => m.hp));
      const { container } = render(
        <div className="world-corner">
          <BossBar />
        </div>,
      );
      act(() => {
        fieldBoss
          .getState()
          .applyFieldBoss(create(FieldBossSchema, { mobId: 9, type: longest.id }));
        fieldBoss.getState().setHp({ hp: widest, maxHp: widest });
      });
      const bar = screen.getByRole('group', { name: 'Field boss' });
      expect(container.querySelector('.world-corner')).toContainElement(bar);
      expect(bar).toHaveClass('unit-frame-compact');
      expect(getComputedStyle(bar)).toMatchObject({
        width: '180px',
        boxSizing: 'border-box',
        paddingLeft: '8px',
        paddingRight: '8px',
      });
      const rule = /\.boss-bar \{([^}]*)\}/.exec(style.textContent ?? '')?.[1] ?? ''; // jsdom keeps no var() in a shorthand, so the stylesheet's text is read
      expect(rule).toMatch(/border: 1px solid var\(--accent\)/);
      expect(rule).toMatch(/border-left: 4px solid var\(--side\)/);
      const head = bar.querySelector('.unit-frame-head')!;
      const name = within(head as HTMLElement).getByText(longest.name);
      expect(getComputedStyle(name)).toMatchObject({
        overflow: 'hidden',
        textOverflow: 'ellipsis',
      });
      expect(head.compareDocumentPosition(screen.getByRole('meter'))).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      ); // the bar is under the name's row
      // the column's 180 px less the border, padding and the 12 px mark and its 6 px gap, at the frame's 12 px font and 0.6 em a glyph, which bold capitals stay under
      const room = 180 - 5 - 16;
      const glyph = 0.6 * 12;
      expect(longest.name.length * glyph + 12 + 6).toBeLessThanOrEqual(room);
      // the bar's numbers sit on its line, "HP" and "hp / hp" at the same width
      const numbers = `HP ${widest} / ${widest}`;
      expect(numbers.length * glyph).toBeLessThanOrEqual(room);
    });
  });
});
