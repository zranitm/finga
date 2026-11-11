import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import { ResponseSchema, StatsChangedSchema, WelcomeSchema } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { refusal } from '@/testing/refusal';
import type { SentRequest } from '@/testing/requests';
import { resetStores } from '@/testing/reset-stores';

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
  mp: 28,
  maxMp: 28,
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
        mp: 28,
        maxMp: 28,
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
    expect(within(beginner).getAllByRole('listitem')).toHaveLength(4);
    expect(row('Strike')).toHaveTextContent('8 points');
    expect(row('Strike')).toHaveTextContent('1.9× physical attack · 6 MP · 2s cooldown');
    expect(row('Strike')).toHaveTextContent('Each point: power +0.05× · MP +0.2');
    expect(row('Strike')).toHaveTextContent('Evolves into Heavy Strike at 10 points (2 more).');
    expect(row('Volley')).toHaveTextContent(
      '3.75× physical attack · 9 MP · 2s cooldown · range 380 · up to 3 foes',
    );
    expect(row('Volley')).toHaveTextContent('Fully evolved.');
    expect(row('Spark')).toHaveTextContent(
      'Usable once it has a point: 1.66× magic attack · 6 MP · 2.5s cooldown · range 220',
    );
    expect(row('Second Wind')).toHaveTextContent(
      'Works once it has a point: HP regen +0.6/s · MP regen +0.6/s',
    );
    expect(row('Second Wind')).toHaveTextContent('Each point: HP regen +0.1/s · MP regen +0.1/s');
    expect(bar()).toHaveTextContent('No skill points to spend');
    expect(screen.queryByRole('button', { name: /^Add a point/ })).toBeNull();
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

  it('+ stages points until none are left, previews the evolution, and Undo returns them', async () => {
    setup();
    skills(2, { strike: 8 });
    await userEvent.click(plus('Strike'));
    await userEvent.click(plus('Strike'));
    expect(plus('Spark')).toBeDisabled();
    expect(bar()).toHaveTextContent('Skill points: 0');
    expect(row('Strike')).toHaveTextContent('Strike → Heavy Strike8 points +2');
    expect(row('Strike')).toHaveTextContent(
      '1.9× physical attack · 6 MP · 2s cooldown → 2.7× physical attack · 6 MP · 2s cooldown',
    );
    expect(row('Strike')).toHaveTextContent('Evolves into Crushing Blow at 25 points (15 more).');
    await userEvent.click(within(bar()).getByRole('button', { name: 'Undo' }));
    expect(row('Strike')).not.toHaveTextContent('→');
    expect(bar()).toHaveTextContent('Skill points: 2');
  });

  it('a skill with no points previews what its staged points make it', async () => {
    setup();
    skills(10, {});
    for (let i = 0; i < 3; i++) await userEvent.click(plus('Strike'));
    expect(row('Strike')).toHaveTextContent(
      'Usable once it has a point → 1.65× physical attack · 5 MP · 2s cooldown',
    );
    for (let i = 0; i < 7; i++) await userEvent.click(plus('Strike'));
    expect(row('Strike')).toHaveTextContent('Strike → Heavy Strike0 points +10');
    expect(row('Strike')).toHaveTextContent('→ 2.7× physical attack · 6 MP · 2s cooldown');
    expect(row('Strike')).toHaveTextContent('A wider, harder swing.');
  });

  it('Apply sends the staged points with the unspent count it saw, and clears them once applied', async () => {
    const { calls } = setup([ok]);
    skills(5, {});
    await userEvent.click(plus('Strike'));
    await userEvent.click(plus('Second Wind'));
    await userEvent.click(plus('Strike'));
    await userEvent.click(within(bar()).getByRole('button', { name: 'Apply' }));
    expect(calls).toEqual([
      { t: 'spendSkillPoints', d: { expectedUnspent: 5, points: { strike: 2, second_wind: 1 } } },
    ]);
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
  });

  it('skill_points_changed means it already landed; another refusal or no answer shows words', async () => {
    setup([
      refusal(ERR.SKILL_POINTS_CHANGED),
      refusal(ERR.SKILL_NOT_LEARNABLE),
      new RequestError('timeout', 'no response'),
    ]);
    skills(5, {});
    const apply = async () => {
      await userEvent.click(plus('Throw'));
      await userEvent.click(within(bar()).getByRole('button', { name: 'Apply' }));
    };
    await apply();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    await apply();
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Your class can’t learn that skill.',
    );
    await userEvent.click(within(bar()).getByRole('button', { name: 'Undo' }));
    await apply();
    expect(await screen.findByRole('alert')).toHaveTextContent('No answer yet. Try again.');
  });

  it('a draft is void once the unspent count changes, so it is never spent from a stale count', async () => {
    setup();
    skills(5, {});
    await userEvent.click(plus('Throw'));
    skills(10, {});
    expect(screen.queryByRole('button', { name: 'Apply' })).toBeNull();
    expect(bar()).toHaveTextContent('Skill points: 10');
  });

  it('closes with its × button', async () => {
    const { onClose } = setup();
    await userEvent.click(screen.getByRole('button', { name: 'Close Skills' }));
    expect(onClose).toHaveBeenCalled();
  });
});
