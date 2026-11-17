import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { skills as skillData } from '@/gamedata/skills';
import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import { ResponseSchema, StatsChangedSchema, WelcomeSchema } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import { dataTransfer } from '@/testing/data-transfer';
import { refusal } from '@/testing/refusal';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { SkillWindow } from './SkillWindow';

const ayla = {
  id: 'c1',
  name: 'Ayla',
  class: 'beginner',
  level: 5,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 100,
  maxHp: 100,
};
const ok = create(ResponseSchema, { ok: true });

/** A StatsChanged with these skill points, the rest of the sheet left out. */
function skills(unspent: number, invested: Record<string, number>) {
  act(() =>
    character.getState().applyStatsChanged(
      create(StatsChangedSchema, {
        sheet: { skills: { unspent, invested } },
        hp: 100,
        maxHp: 100,
      }),
    ),
  );
}

function setup(answers: readonly (Data<'response'> | Error)[] = []) {
  const calls: SentRequest[] = [];
  const request = vi.fn((t: string, d: Record<string, unknown>) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? ok;
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  const onClose = vi.fn();
  render(<SkillWindow socket={{ request } as never} onClose={onClose} />);
  return { calls, onClose };
}

const row = (name: string) =>
  screen.getByText(name, { selector: '.skill-head strong' }).closest('li') as HTMLElement;
const plus = (name: string) => screen.getByRole('button', { name: `Add a point to ${name}` });
const bar = () => screen.getByRole('group', { name: 'Skill points' });

describe('SkillWindow', () => {
  beforeEach(() => {
    resetStores();
    character.getState().applyWelcome(create(WelcomeSchema, { token: 't', character: ayla }));
  });

  it('lists the Beginner’s skills, each with its points, what it does, what a point adds and its next form', () => {
    setup();
    skills(0, { strike: 8, throw: 25 });
    const beginner = screen.getByRole('region', { name: 'Beginner skills' });
    expect(within(beginner).getAllByRole('listitem')).toHaveLength(6);
    expect(row('Strike')).toHaveTextContent('8 points');
    expect(row('Strike')).toHaveTextContent('1.9× physical attack · 2 energy · 4.5s cooldown');
    expect(row('Strike')).toHaveTextContent('Takes 1s. A basic attack can cut it short.');
    expect(row('Strike')).toHaveTextContent('Each point: power +0.05×');
    expect(row('Strike')).toHaveTextContent('Evolves into Heavy Strike at 10 points (2 more).');
    expect(row('Volley')).toHaveTextContent(
      '3.75× physical attack · 2 energy · 4.5s cooldown · range 11.875 · up to 3 foes',
    );
    expect(row('Volley')).toHaveTextContent('Fully evolved.');
    expect(row('Spark')).toHaveTextContent(
      'Usable once it has a point: 1.66× magic attack · 2 energy · 5.65s cooldown · range 6.875',
    );
    expect(row('Second Wind')).toHaveTextContent('Works once it has a point: HP regen +0.6/s');
    expect(row('Second Wind')).toHaveTextContent('Each point: HP regen +0.1/s');
    expect(row('Second Wind')).not.toHaveTextContent('Takes');
    expect(bar()).toHaveTextContent('No skill points to spend');
    expect(screen.queryByRole('button', { name: /^Add a point/ })).toBeNull();
  });

  it('leads each row with its skill’s icon for its stage, greyed until it has a point, with the bar’s cooldown on it', () => {
    vi.spyOn(performance, 'now').mockReturnValue(1000);
    skillBar.getState().restoreTimers({ strike: { from: 0, until: 4000 } }, [], 50);
    setup();
    skills(0, { strike: 8, throw: 25 });
    const icon = (name: string) => row(name).querySelector<HTMLElement>('.skill-head .skill-icon');
    expect(icon('Strike')?.style.getPropertyValue('--icon')).toContain('lorc/sword-wound.svg');
    expect(icon('Volley')?.style.getPropertyValue('--icon')).toContain('lorc/thrown-charcoal.svg');
    expect(icon('Second Wind')?.style.getPropertyValue('--icon')).toContain('lorc/wind-hole.svg');
    expect([icon('Strike')?.className, icon('Spark')?.className]).toEqual([
      'skill-icon skill-icon-window',
      'skill-icon skill-icon-window skill-icon-off',
    ]);
    expect(icon('Strike')?.querySelector('.skill-icon-count')).toHaveTextContent('3');
    // a pip a stage reached: Volley's three at 25 points, and none on Spark with no points yet
    expect([
      icon('Volley')?.querySelectorAll('.skill-icon-pip').length,
      icon('Spark')?.querySelectorAll('.skill-icon-pip').length,
    ]).toEqual([3, 0]);
    expect(icon('Volley')?.querySelector('.skill-icon-count')).toBeNull();
    vi.restoreAllMocks();
  });

  it('shows the cooldown the skill bar runs, cut by the sheet’s cooldown speed', () => {
    setup();
    act(() =>
      character.getState().applyStatsChanged(
        create(StatsChangedSchema, {
          sheet: {
            skills: { unspent: 0, invested: { strike: 8 } },
            derived: { cooldownSpeed: 0.1 },
          },
          hp: 100,
          maxHp: 100,
        }),
      ),
    );
    expect(row('Strike')).toHaveTextContent('1.9× physical attack · 2 energy · 4.05s cooldown');
  });

  it('a Warrior still learns the Beginner’s skills', () => {
    act(() =>
      character
        .getState()
        .applyWelcome(
          create(WelcomeSchema, { token: 't', character: { ...ayla, class: 'warrior' } }),
        ),
    );
    setup();
    expect(screen.getByRole('region', { name: 'Beginner skills' })).toBeInTheDocument();
    expect(row('Strike')).toHaveTextContent('0 points');
  });

  it.each([
    {
      cls: 'bowman',
      name: 'Focus',
      text: 'Usable once it has a point: Accuracy +22 and Crit rate +5.5% on you for 30s · 3 energy · 60s cooldown',
    },
    {
      cls: 'magician',
      name: 'Heal',
      text: 'Usable once it has a point: Heals you and the nearest hurt party members within 4.6875, up to 3 in all, by 1.55× magic attack · 3 energy · 4.5s cooldown',
    },
  ])('a $cls reads $name by what it does, not as an attack', ({ cls, name, text }) => {
    act(() =>
      character
        .getState()
        .applyWelcome(create(WelcomeSchema, { token: 't', character: { ...ayla, class: cls } })),
    );
    setup();
    expect(row(name)).toHaveTextContent(text);
    expect(row(name)).not.toHaveTextContent('foes');
  });

  it('+ stages points until none are left, previews the evolution, and Undo returns them', async () => {
    setup();
    skills(2, { strike: 8 });
    await user.click(plus('Strike'));
    await user.click(plus('Strike'));
    expect(plus('Spark')).toBeDisabled();
    expect(bar()).toHaveTextContent('Skill points: 0');
    expect(row('Strike')).toHaveTextContent('Strike → Heavy Strike8 points +2');
    expect(row('Strike')).toHaveTextContent(
      '1.9× physical attack · 2 energy · 4.5s cooldown · radius 2.375 → 2.7× physical attack · 2 energy · 4.5s cooldown · radius 2.875',
    );
    expect(row('Strike')).toHaveTextContent('Evolves into Crushing Blow at 25 points (15 more).');
    await user.click(within(bar()).getByRole('button', { name: 'Undo' }));
    expect(row('Strike')).not.toHaveTextContent('→');
    expect(bar()).toHaveTextContent('Skill points: 2');
  });

  it('a skill with no points previews what its staged points make it', async () => {
    setup();
    skills(10, {});
    for (let i = 0; i < 3; i++) await user.click(plus('Strike'));
    expect(row('Strike')).toHaveTextContent('Usable once it has a point');
    expect(row('Strike')).toHaveTextContent(
      'With 3 points: 1.65× physical attack · 2 energy · 4.5s cooldown',
    );
    expect(row('Strike')).not.toHaveTextContent('point →');
    for (let i = 0; i < 7; i++) await user.click(plus('Strike'));
    expect(row('Strike')).toHaveTextContent('Strike → Heavy Strike0 points +10');
    expect(row('Strike')).toHaveTextContent(
      'With 10 points: 2.7× physical attack · 2 energy · 4.5s cooldown',
    );
    expect(row('Strike')).toHaveTextContent('A wider, harder swing.');
  });

  it('an active skill drags its ID to the skill bar; a passive one stays put', () => {
    setup();
    skills(0, { strike: 1 });
    const carried = dataTransfer();
    const image = vi.spyOn(carried, 'setDragImage');
    fireEvent.dragStart(row('Strike'), { dataTransfer: carried });
    expect(carried.getData(SKILL_DRAG_TYPE)).toBe('strike');
    expect(image).toHaveBeenCalledWith(row('Strike').querySelector('.skill-icon'), 0, 0); // its centre, 0 in jsdom
    expect(row('Strike')).toHaveAttribute('draggable', 'true');
    expect(row('Second Wind')).toHaveAttribute('draggable', 'false');
  });

  it('Apply sends the staged points with the unspent count it saw, and clears them once applied', async () => {
    const { calls } = setup([ok]);
    skills(5, {});
    await user.click(plus('Strike'));
    await user.click(plus('Second Wind'));
    await user.click(plus('Strike'));
    await user.click(within(bar()).getByRole('button', { name: 'Apply' }));
    expect(calls).toEqual([
      { t: 'spendSkillPoints', d: { expectedUnspent: 5, points: { strike: 2, second_wind: 1 } } },
    ]);
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull()); // once the answer lands
  });

  it('skill_points_changed means it already landed; another refusal or no answer shows words', async () => {
    setup([
      refusal(ERR.SKILL_POINTS_CHANGED),
      refusal(ERR.SKILL_NOT_LEARNABLE),
      new RequestError('timeout', 'no response'),
    ]);
    skills(5, {});
    const apply = async () => {
      await user.click(plus('Throw'));
      await user.click(within(bar()).getByRole('button', { name: 'Apply' }));
    };
    await apply();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull()); // once the answer lands
    expect(screen.queryByRole('alert')).toBeNull();
    await apply();
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Your class can’t learn that skill.',
    );
    await user.click(within(bar()).getByRole('button', { name: 'Undo' }));
    await apply();
    expect(await screen.findByRole('alert')).toHaveTextContent('No answer yet. Try again.');
  });

  it('a draft is void once the unspent count changes, so it is never spent from a stale count', async () => {
    setup();
    skills(5, {});
    await user.click(plus('Throw'));
    skills(10, {});
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    expect(bar()).toHaveTextContent('Skill points: 10');
  });

  it('a skill below its level shows the level it opens at in place of its +, and takes points from that level on', () => {
    const spark = skillData.find((s) => s.id === 'spark') as { level: number }; // the shipped skills all open at level 1
    const was = spark.level;
    spark.level = 6;
    try {
      setup();
      skills(3, {});
      expect(within(row('Spark')).getByText('Lv 6')).toHaveAttribute(
        'title',
        'Takes its first point at level 6',
      );
      expect(
        screen.queryByRole('button', { name: 'Add a point to Spark' }),
      ).not.toBeInTheDocument();
      expect(plus('Strike')).toBeEnabled();
      act(() => {
        character
          .getState()
          .applyWelcome(create(WelcomeSchema, { token: 't', character: { ...ayla, level: 6 } }));
      });
      skills(3, {});
      expect(plus('Spark')).toBeEnabled();
      expect(within(row('Spark')).queryByText('Lv 6')).not.toBeInTheDocument();
    } finally {
      spark.level = was;
    }
  });

  it('closes with its × button', async () => {
    const { onClose } = setup();
    await user.click(screen.getByRole('button', { name: 'Close Skills' }));
    expect(onClose).toHaveBeenCalled();
  });
});
