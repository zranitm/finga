import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { skillKeys } from '@/input/actions';
import { WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { energy } from '@/state/energy';
import { keyboardLayout } from '@/state/keyboard-layout';
import { settings } from '@/state/settings';
import { SKILL_DRAG_TYPE, SKILL_KEYS, skillBar } from '@/state/skill-bar';
import { dataTransfer } from '@/testing/data-transfer';
import { resetStores } from '@/testing/reset-stores';

import { SkillBar } from './SkillBar';

/** A warrior with 2 energy, one point in Strike (2) and Slash Blast (3), and 10 in Spark (Bolt), or the points and traits given. */
function joined(
  invested: Record<string, number> = { strike: 1, slash_blast: 1, spark: 10 },
  traits: Record<string, string> = {},
) {
  character.getState().applyWelcome(
    create(WelcomeSchema, {
      token: 'tok',
      character: {
        id: 'c-ayla',
        name: 'Ayla',
        class: 'warrior',
        level: 3,
        hp: 150,
        maxHp: 204,
        sheet: { skills: { unspent: 0, invested, traits } },
      },
      mapId: 'town',
      tick: 1,
    }),
  );
  energy.getState().welcome(2);
}

/** The bar's key n from 1, by the key it's bound to. */
const key = (n: number) =>
  screen.getByRole('listitem', { name: new RegExp(`^Key ${'QWERASDF'[n - 1]}:`) });

describe('SkillBar', () => {
  beforeEach(() => {
    resetStores();
    joined();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('has eight keys, each labelled with the key it’s bound to: Q W E R A S D F', () => {
    skillBar.getState().load(['strike']);
    render(<SkillBar />);
    const keys = within(screen.getByRole('list', { name: 'Skill bar' })).getAllByRole('listitem');
    expect(keys.map((k) => k.querySelector('.skill-key-bound')?.textContent)).toEqual([
      'Q',
      'W',
      'E',
      'R',
      'A',
      'S',
      'D',
      'F',
    ]);
    expect(skillKeys).toHaveLength(SKILL_KEYS); // a key bound for each slot the bar holds
    expect(key(2)).toHaveAttribute(
      'title',
      'Key W: empty. Drag a skill here from the skill window (K).',
    );
  });

  it('labels each key with the key the player bound, as their keyboard prints it', () => {
    settings.getState().setKeys({ skill1: 'KeyB' });
    keyboardLayout.getState().setPrinted(new Map([['KeyW', 'z']]));
    skillBar.getState().load(['strike']);
    render(<SkillBar />);
    expect(screen.getByRole('listitem', { name: /^Key B: Strike/ })).toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: /^Key Z: empty/ })).toBeInTheDocument();
    act(() => {
      settings.getState().setKeys({});
    });
    expect(screen.getByRole('listitem', { name: /^Key Q: Strike/ })).toBeInTheDocument();
  });

  it('shows a long key name short on the key, so it never covers the icon, and in full in its label', () => {
    settings.getState().setKeys({ skill1: 'ShiftRight', skill2: 'ControlRight' });
    skillBar.getState().load(['strike']);
    render(<SkillBar />);
    const key = screen.getByRole('listitem', { name: /^Key Right Shift: Strike/ });
    expect(key.querySelector('.skill-key-bound')).toHaveTextContent(/^RShift$/);
    expect(
      screen
        .getByRole('listitem', { name: /^Key Right Ctrl: empty/ })
        .querySelector('.skill-key-bound'),
    ).toHaveTextContent(/^RCtrl$/);
  });

  it('shows each key with its skill’s stage and cost, and greys a skill short of energy or without points', () => {
    skillBar.getState().load(['strike', 'slash_blast', 'throw', '', '', '', '', '']);
    render(<SkillBar />);
    expect(key(1)).toHaveAccessibleName('Key Q: Strike, 2 energy');
    expect(key(2)).toHaveAccessibleName('Key W: Slash Blast, 3 energy');
    expect(key(1)).toHaveTextContent('2');
    expect(key(2)).toHaveTextContent('3');
    expect(key(3)).toHaveAccessibleName('Key E: Throw, no points yet');
    expect(key(4)).toHaveAccessibleName('Key R: empty');
    expect(key(1)).not.toHaveClass('skill-key-off');
    expect(key(2)).toHaveClass('skill-key-off');
    expect(key(3)).toHaveClass('skill-key-off');
    act(() => {
      energy.getState().spend(1, 2); // a press takes it off at once
    });
    expect(key(1)).toHaveClass('skill-key-off');
  });

  it('a key whose skill uses its extra trait shows a diamond in its colour, and its tooltip names the trait', () => {
    joined(
      { strike: 25, slash_blast: 9, spark: 10 },
      { strike: 'concussion', slash_blast: 'whirlwind' },
    );
    skillBar.getState().load(['strike', 'slash_blast', 'spark', '', '', '', '', '']);
    render(<SkillBar />);
    expect(key(1)).toHaveAccessibleName('Key Q: Crushing Blow with Concussion, 2 energy');
    expect(key(1)).toHaveAttribute('title', 'Key Q: Crushing Blow with Concussion, 2 energy');
    expect(key(1).querySelector('.skill-key-trait')).not.toBeNull();
    expect(key(1).style.getPropertyValue('--skill')).toBe('#ff6a3d');
    expect(key(2)).toHaveAccessibleName('Key W: Slash Blast, 3 energy'); // Whirlwind opens at Rending Blast
    expect(key(2).querySelector('.skill-key-trait')).toBeNull();
    expect(key(3).querySelector('.skill-key-trait')).toBeNull();
  });

  it('a skill dropped from the skill window goes on the key, and leaves any key it was on', () => {
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    render(<SkillBar />);
    const carried = dataTransfer();
    carried.setData(SKILL_DRAG_TYPE, 'strike');
    fireEvent.drop(key(4), { dataTransfer: carried });
    expect(skillBar.getState().slots).toEqual(['', '', '', 'strike', '', '', '', '']);
    fireEvent.drop(key(5), { dataTransfer: dataTransfer() }); // carries nothing of ours
    expect(skillBar.getState().edits).toBe(1);
  });

  it('a key whose chain offers a follow-up now shows it in a frame glowing its colour, with its own cooldown, and says on whose key and while what', () => {
    joined({ strike: 10, charge: 1 });
    vi.spyOn(performance, 'now').mockReturnValue(1000);
    skillBar.getState().load(['strike', 'slash_blast', '', '', '', '', '', '']);
    skillBar.getState().cool('charge', { from: 0, until: 4000 });
    render(<SkillBar />);
    expect(key(1)).not.toHaveClass('skill-key-chain');
    act(() => {
      skillBar.getState().showLive(['charge', 'slash_blast', '', '', '', '', '', '']);
    });
    expect(key(1)).toHaveAccessibleName(
      'Key Q: Charge, 3 energy, on Heavy Strike’s key while the target is over 3 m away',
    );
    expect(key(1)).toHaveClass('skill-key-chain');
    expect(key(1).style.getPropertyValue('--chain')).toBe('#ff6a3d');
    const icon = key(1).querySelector<HTMLElement>('.skill-icon');
    expect(icon?.style.getPropertyValue('--icon')).toContain('delapouite/charging-bull.svg');
    expect(key(1).querySelector('.skill-icon-count')).toHaveTextContent('3'); // Charge's cooldown, 3 s left
    expect(key(2)).not.toHaveClass('skill-key-chain');
    act(() => {
      skillBar.getState().showLive(['strike', 'slash_blast', '', '', '', '', '', '']); // the condition ended
    });
    expect(key(1)).toHaveAccessibleName('Key Q: Heavy Strike, 2 energy');
    expect(key(1)).not.toHaveClass('skill-key-chain');
  });

  it('a held Warrior’s escape key offers its break-free, Unyielding, lit and glowing, granted with no points, while what says held', () => {
    joined({ roll_back: 1 });
    skillBar.getState().load(['roll_back', '', '', '', '', '', '', '']);
    render(<SkillBar />);
    act(() => {
      skillBar.getState().showLive(['unyielding', '', '', '', '', '', '', '']);
    });
    expect(key(1)).toHaveAccessibleName(
      'Key Q: Unyielding, 0 energy, on Roll Back’s key while you’re held: stunned, frozen, asleep, knocked down or pinned',
    );
    expect(key(1)).toHaveClass('skill-key-chain');
    expect(key(1)).not.toHaveClass('skill-key-off');
  });

  it('refuses a follow-up dropped on a key, which flashes red and keeps its skill', () => {
    vi.useFakeTimers();
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    render(<SkillBar />);
    const charge = dataTransfer();
    charge.setData(SKILL_DRAG_TYPE, 'charge'); // Strike's approach for a Warrior
    fireEvent.drop(key(1), { dataTransfer: charge });
    expect(skillBar.getState().slots[0]).toBe('strike');
    expect(key(1).querySelector('.skill-key-refused')).not.toBeNull();
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(key(1).querySelector('.skill-key-refused')).toBeNull();
    const taunt = dataTransfer();
    taunt.setData(SKILL_DRAG_TYPE, 'taunt'); // in no chain
    fireEvent.drop(key(2), { dataTransfer: taunt });
    expect(skillBar.getState().slots[1]).toBe('taunt');
    vi.useRealTimers();
  });

  // A browser drops only with an effect the drag allows; the skill window's rows allow copy.
  it('a key answers a skill from the skill window with copy, and a dragged key with move', () => {
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    render(<SkillBar />);
    const skill = dataTransfer();
    skill.setData(SKILL_DRAG_TYPE, 'spark');
    fireEvent.dragOver(key(2), { dataTransfer: skill });
    expect(skill.dropEffect).toBe('copy');
    const moved = dataTransfer();
    const image = vi.spyOn(moved, 'setDragImage');
    fireEvent.dragStart(key(1), { dataTransfer: moved });
    expect(image).toHaveBeenCalledWith(key(1).querySelector('.skill-icon'), 0, 0); // its centre, 0 in jsdom
    fireEvent.dragOver(key(2), { dataTransfer: moved });
    fireEvent.dragEnd(key(1), { dataTransfer: moved });
    expect([moved.effectAllowed, moved.dropEffect]).toEqual(['move', 'move']);
  });

  it('a key dragged onto another swaps them; dropped off the bar, it empties; a drag cancelled with Esc keeps it', () => {
    skillBar.getState().load(['strike', 'spark', '', '', '', '', '', '']);
    render(<SkillBar />);
    const drag = (n: number, drop?: Element) => {
      const carried = dataTransfer();
      fireEvent.dragStart(key(n), { dataTransfer: carried });
      if (drop) {
        fireEvent.drop(drop, { dataTransfer: carried });
      }
      fireEvent.dragEnd(key(n), { dataTransfer: carried });
    };
    drag(1, key(2));
    expect(skillBar.getState().slots).toEqual(['spark', 'strike', '', '', '', '', '', '']);
    drag(2); // Esc: it drops nowhere
    expect(skillBar.getState().slots).toEqual(['spark', 'strike', '', '', '', '', '', '']);
    drag(2, document.body);
    expect(skillBar.getState().slots).toEqual(['spark', '', '', '', '', '', '', '']);
    fireEvent.drop(document.body, { dataTransfer: dataTransfer() }); // no key is dragged
    expect(skillBar.getState().slots).toEqual(['spark', '', '', '', '', '', '', '']);
  });

  it('shows each skill’s icon for its stage in place of its name, which the label and the tooltip carry', () => {
    skillBar.getState().load(['strike', 'spark', '', '', '', '', '', '']);
    render(<SkillBar />);
    const icon = (n: number) => key(n).querySelector<HTMLElement>('.skill-icon');
    expect(icon(1)?.style.getPropertyValue('--icon')).toContain('lorc/sword-wound.svg');
    expect(icon(2)?.style.getPropertyValue('--icon')).toContain('lorc/focused-lightning.svg'); // 10 points: Bolt
    expect(key(1)).not.toHaveTextContent('Strike');
    expect(key(1)).toHaveAttribute('title', 'Key Q: Strike, 2 energy');
    expect(key(2)).toHaveAccessibleName('Key W: Bolt, 2 energy');
    expect([
      icon(1)?.classList.contains('skill-icon-off'),
      icon(2)?.classList.contains('skill-icon-off'),
    ]).toEqual([false, false]);
    act(() => {
      energy.getState().spend(1, 1);
    });
    expect(icon(1)).toHaveClass('skill-icon-off'); // short of energy
    expect(icon(3)).toBeNull();
    expect(key(3)).toHaveClass('skill-key-empty');
  });

  it('a cooling skill shows its sweep and count over its icon until it ends', () => {
    vi.spyOn(performance, 'now').mockReturnValue(100);
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    render(<SkillBar />);
    act(() => skillBar.getState().cool('strike', { from: 100, until: 2100 }));
    expect(key(1).querySelector('.skill-icon-count')).toHaveTextContent('2');
    act(() => skillBar.getState().cool('strike', null));
    expect(key(1).querySelector('.skill-icon-sweep')).toBeNull();
  });

  it('a cooldown restored at a join starts part-way: 10 s left of 60', () => {
    vi.spyOn(performance, 'now').mockReturnValue(500_000);
    skillBar.getState().load(['strike', '', '', '', '', '', '', '']);
    skillBar.getState().restoreTimers({ strike: { from: 450_000, until: 510_000 } }, [], 50);
    render(<SkillBar />);
    expect(key(1).querySelector('.skill-icon-count')).toHaveTextContent('10');
    expect(
      key(1).querySelector<HTMLElement>('.skill-icon-sweep')?.style.getPropertyValue('--left'),
    ).toBe('60deg');
  });
});
