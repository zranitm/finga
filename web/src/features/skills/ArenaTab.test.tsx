import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { WelcomeSchema } from '@/protocol/messages';
import { arenaBuild } from '@/state/arena-build';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import { dataTransfer } from '@/testing/data-transfer';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { SkillWindow } from './SkillWindow';

/** Ayla as cls, with points to spend and Strike's world trait Swift. */
function joined(cls: string) {
  character.getState().applyWelcome(
    create(WelcomeSchema, {
      character: {
        id: 'c-ayla',
        name: 'Ayla',
        class: cls,
        level: 12,
        hp: 100,
        maxHp: 100,
        sheet: { skills: { unspent: 3, invested: { strike: 1 }, traits: {} } },
      },
    }),
  );
}

function open() {
  render(<SkillWindow socket={{ request: vi.fn() } as never} onClose={vi.fn()} />);
}

const arenaKey = (n: number) =>
  within(screen.getByRole('list', { name: 'Arena bar' })).getByRole('listitem', {
    name: new RegExp(`^Key ${'QWERASDF'[n - 1]}:`),
  });

/** Drags what carries onto the element, or nowhere. */
function drop(on: Element, carried: DataTransfer) {
  fireEvent.dragOver(on, { dataTransfer: carried });
  fireEvent.drop(on, { dataTransfer: carried });
}

describe('the skill window’s Arena tab', () => {
  beforeEach(() => {
    resetStores();
  });

  it('a Beginner has no Arena tab, as only a promoted character fights in PvP', () => {
    joined('beginner');
    open();
    expect(screen.queryByRole('tablist')).toBeNull();
    expect(screen.getByRole('group', { name: 'Skill points' })).toBeInTheDocument();
  });

  it('lists the class’s active skills at full, with no points to spend, once the build is read', async () => {
    joined('warrior');
    open();
    await user.click(screen.getByRole('tab', { name: 'Arena' }));
    expect(screen.getByText('Reading your arena build…')).toBeInTheDocument();
    act(() => arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} }));
    expect(screen.getByText(/level 30, every skill full and every trait open/)).toBeInTheDocument();
    const beginner = screen.getByRole('region', { name: 'Beginner skills' });
    expect(within(beginner).getByRole('listitem', { name: 'Crushing Blow' })).toBeInTheDocument(); // Strike's last stage
    expect(
      within(beginner).queryByRole('listitem', { name: /Second Wind|Deep Breath/ }),
    ).toBeNull(); // a passive has no place in a build
    expect(screen.queryByRole('button', { name: /Add a point/ })).toBeNull();
    expect(screen.queryByRole('group', { name: 'Skill points' })).toBeNull();
    await user.click(screen.getByRole('tab', { name: 'Skills' }));
    expect(screen.getByRole('group', { name: 'Skill points' })).toBeInTheDocument();
  });

  it('every trait is open to choose, a follow-up’s too, and the default goes back to none', async () => {
    joined('warrior');
    arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} });
    open();
    await user.click(screen.getByRole('tab', { name: 'Arena' }));
    const strike = screen.getByRole('group', { name: 'Crushing Blow’s traits' });
    await user.click(within(strike).getByRole('button', { name: 'Concussion' }));
    await user.click(
      within(screen.getByRole('group', { name: 'Iron Body’s traits' })).getByRole('button', {
        name: 'Iron Wall',
      }),
    );
    expect(arenaBuild.getState().traits).toEqual({ strike: 'concussion', iron_body: 'iron_wall' });
    expect(within(strike).getByRole('button', { name: 'Concussion, chosen' })).toBeInTheDocument();
    await user.click(within(strike).getByRole('button', { name: 'Swift' }));
    expect(arenaBuild.getState().traits).toEqual({ iron_body: 'iron_wall' });
  });

  it('a skill dropped on the arena bar goes on its key, a follow-up is refused, keys swap, and one dropped off the bar empties', async () => {
    joined('warrior');
    skillBar.getState().load(['taunt']);
    arenaBuild.getState().load({ skill_bar: ['strike'], traits: {} });
    open();
    await user.click(screen.getByRole('tab', { name: 'Arena' }));
    expect(arenaKey(1)).toHaveAccessibleName('Key Q: Crushing Blow');
    const skill = (id: string) => {
      const d = dataTransfer();
      d.setData(SKILL_DRAG_TYPE, id);
      return d;
    };
    drop(arenaKey(2), skill('war_cry'));
    drop(arenaKey(3), skill('iron_body')); // War Cry's follow-up
    expect(arenaBuild.getState().bar).toEqual(['strike', 'war_cry', '', '', '', '', '', '']);
    const moved = dataTransfer();
    fireEvent.dragStart(arenaKey(2), { dataTransfer: moved });
    expect(moved.effectAllowed).toBe('move');
    drop(arenaKey(1), moved);
    fireEvent.dragEnd(arenaKey(2), { dataTransfer: moved });
    expect(arenaBuild.getState().bar).toEqual(['war_cry', 'strike', '', '', '', '', '', '']);
    const edits = arenaBuild.getState().edits;
    const still = dataTransfer();
    fireEvent.dragStart(arenaKey(1), { dataTransfer: still });
    drop(arenaKey(1), still); // onto itself: no change to save
    fireEvent.dragEnd(arenaKey(1), { dataTransfer: still });
    expect(arenaBuild.getState().edits).toBe(edits);
    const off = dataTransfer();
    fireEvent.dragStart(arenaKey(1), { dataTransfer: off });
    fireEvent.drop(document.body, { dataTransfer: off });
    fireEvent.dragEnd(arenaKey(1), { dataTransfer: off });
    expect(arenaBuild.getState().bar).toEqual(['', 'strike', '', '', '', '', '', '']);
    expect(skillBar.getState().world[0]).toBe('taunt'); // the world bar's untouched
  });
});
