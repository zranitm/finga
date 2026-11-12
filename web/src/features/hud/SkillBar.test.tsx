import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import { dataTransfer } from '@/testing/data-transfer';
import { resetStores } from '@/testing/reset-stores';

import { SkillBar } from './SkillBar';

/** A warrior with 5 MP, one point in Strike and 10 in Spark (Bolt, 9 MP). */
function joined() {
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
        mp: 5,
        maxMp: 28,
        sheet: { skills: { unspent: 0, invested: { strike: 1, spark: 10 } } },
      },
      mapId: 'town',
      tick: 1,
    }),
  );
}

const key = (n: number) => screen.getByRole('listitem', { name: new RegExp(`^Key ${n}:`) });

describe('SkillBar', () => {
  beforeEach(() => {
    resetStores();
    joined();
  });

  it('shows each key with its skill’s stage, and greys a skill short of MP or without points', () => {
    skillBar.getState().load(['strike', 'spark', 'throw', '', '', '']);
    render(<SkillBar />);
    expect(key(1)).toHaveAccessibleName('Key 1: Strike, 4 MP');
    expect(key(2)).toHaveAccessibleName('Key 2: Bolt, 9 MP');
    expect(key(3)).toHaveAccessibleName('Key 3: Throw, no points yet');
    expect(key(4)).toHaveAccessibleName('Key 4: empty');
    expect(key(1)).not.toHaveClass('skill-key-off');
    expect(key(2)).toHaveClass('skill-key-off');
    expect(key(3)).toHaveClass('skill-key-off');
  });

  it('a skill dropped from the skill window goes on the key, and leaves any key it was on', () => {
    skillBar.getState().load(['strike', '', '', '', '', '']);
    render(<SkillBar />);
    const carried = dataTransfer();
    carried.setData(SKILL_DRAG_TYPE, 'strike');
    fireEvent.drop(key(4), { dataTransfer: carried });
    expect(skillBar.getState().slots).toEqual(['', '', '', 'strike', '', '']);
    fireEvent.drop(key(5), { dataTransfer: dataTransfer() }); // carries nothing of ours
    expect(skillBar.getState().edits).toBe(1);
  });

  // A browser drops only with an effect the drag allows; the skill window's rows allow copy.
  it('a key answers a skill from the skill window with copy, and a dragged key with move', () => {
    skillBar.getState().load(['strike', '', '', '', '', '']);
    render(<SkillBar />);
    const skill = dataTransfer();
    skill.setData(SKILL_DRAG_TYPE, 'spark');
    fireEvent.dragOver(key(2), { dataTransfer: skill });
    expect(skill.dropEffect).toBe('copy');
    const moved = dataTransfer();
    fireEvent.dragStart(key(1), { dataTransfer: moved });
    fireEvent.dragOver(key(2), { dataTransfer: moved });
    fireEvent.dragEnd(key(1), { dataTransfer: moved });
    expect([moved.effectAllowed, moved.dropEffect]).toEqual(['move', 'move']);
  });

  it('a key dragged onto another swaps them; dropped off the bar, it empties; a drag cancelled with Esc keeps it', () => {
    skillBar.getState().load(['strike', 'spark', '', '', '', '']);
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
    expect(skillBar.getState().slots).toEqual(['spark', 'strike', '', '', '', '']);
    drag(2); // Esc: it drops nowhere
    expect(skillBar.getState().slots).toEqual(['spark', 'strike', '', '', '', '']);
    drag(2, document.body);
    expect(skillBar.getState().slots).toEqual(['spark', '', '', '', '', '']);
    fireEvent.drop(document.body, { dataTransfer: dataTransfer() }); // no key is dragged
    expect(skillBar.getState().slots).toEqual(['spark', '', '', '', '', '']);
  });

  it('a cooling skill drains over its cooldown until it ends', () => {
    skillBar.getState().load(['strike', '', '', '', '', '']);
    render(<SkillBar />);
    act(() => skillBar.getState().cool('strike', { from: 100, until: 2100 }));
    expect(key(1).querySelector('.skill-key-cooldown')).toHaveStyle({
      animationDuration: '2000ms',
    });
    act(() => skillBar.getState().cool('strike', null));
    expect(key(1).querySelector('.skill-key-cooldown')).toBeNull();
  });
});
