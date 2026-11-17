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

const tile = (name: string) => screen.getByRole('listitem', { name });
/** The card of the skill named, shown by hovering its tile. */
const card = (name: string, at = { clientX: 100, clientY: 50 }) => {
  fireEvent.mouseEnter(tile(name), at);
  return screen.getByRole('tooltip');
};
const names = (section: string) =>
  within(screen.getByRole('region', { name: section }))
    .getAllByRole('listitem')
    .filter((li) => li.classList.contains('skill-tile'))
    .map((li) => li.getAttribute('aria-label'));
const plus = (name: string) => screen.getByRole('button', { name: `Add a point to ${name}` });
const bar = () => screen.getByRole('group', { name: 'Skill points' });

describe('SkillWindow', () => {
  beforeEach(() => {
    resetStores();
    character.getState().applyWelcome(create(WelcomeSchema, { token: 't', character: ayla }));
  });

  it('shows the Beginner’s skills as tiles in the order their levels open them, each tile its points and name', () => {
    setup();
    skills(0, { strike: 8, throw: 25 });
    expect(names('Beginner skills')).toEqual([
      'Strike',
      'Volley',
      'Spark',
      'Second Wind',
      'Break Free',
      'Lunge',
      'Roll Back',
    ]);
    expect(tile('Strike')).toHaveTextContent('8Strike');
    expect(tile('Spark')).toHaveTextContent('0Spark');
    expect(tile('Break Free')).toHaveTextContent(/^Break Free$/); // granted: no points to show
    expect(tile('Strike').querySelector('.skill-tile-name')).toHaveAttribute('title', 'Strike'); // cut short to fit, whole on hover
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('a card tells a skill’s points, what it does, what a point adds and its next form', () => {
    setup();
    skills(0, { strike: 8, throw: 25 });
    expect(card('Strike')).toHaveTextContent('Beginner · Active · 8 points');
    expect(card('Strike')).toHaveTextContent('1.9× physical attack · 2 energy · 4.5s cooldown');
    expect(card('Strike')).toHaveTextContent('Takes 1s. A basic attack can cut it short.');
    expect(card('Strike')).toHaveTextContent('Each point: power +0.05×');
    expect(card('Strike')).toHaveTextContent('Evolves into Heavy Strike at 10 points (2 more).');
    expect(card('Strike')).toHaveTextContent('Drag onto the skill bar to use it with keys 1 to 6.');
    expect(card('Volley')).toHaveTextContent(
      '3.75× physical attack · 2 energy · 4.5s cooldown · range 11.875 · up to 3 foes',
    );
    expect(card('Volley')).toHaveTextContent('Fully evolved.');
    expect(card('Spark')).toHaveTextContent(
      'Usable once it has a point: 1.66× magic attack · 2 energy · 5.65s cooldown · range 6.875',
    );
    expect(card('Second Wind')).toHaveTextContent('Beginner · Passive · 0 points');
    expect(card('Second Wind')).toHaveTextContent('Works once it has a point: HP regen +0.6/s');
    expect(card('Second Wind')).toHaveTextContent('Each point: HP regen +0.1/s');
    expect(card('Second Wind')).not.toHaveTextContent('Takes');
    expect(card('Second Wind')).not.toHaveTextContent('Drag');
    expect(bar()).toHaveTextContent('No skill points to spend');
    expect(screen.queryByRole('button', { name: /^Add a point/ })).toBeNull();
  });

  it('shows the card beside the pointer as it moves, and on keyboard focus beside the tile, until it leaves', () => {
    setup();
    skills(0, { strike: 8 });
    const shown = () => screen.queryByRole('tooltip');
    const box = () => shown()?.parentElement;
    fireEvent.mouseEnter(tile('Strike'), { clientX: 100, clientY: 50 });
    expect(shown()).toHaveTextContent('Strike');
    expect(tile('Strike')).toHaveAttribute('aria-describedby', shown()?.id);
    expect([box()?.style.left, box()?.style.top]).toEqual(['116px', '66px']); // below and right by 16
    fireEvent.mouseMove(tile('Strike'), { clientX: 200, clientY: 80 });
    expect([box()?.style.left, box()?.style.top]).toEqual(['216px', '96px']);
    fireEvent.mouseLeave(tile('Strike'));
    expect(shown()).toBeNull();
    expect(tile('Strike')).not.toHaveAttribute('aria-describedby');
    act(() => tile('Strike').focus()); // a tile takes focus, as Tab gives it
    expect(shown()).toHaveTextContent('Strike');
    act(() => tile('Throw').focus());
    expect(shown()).toHaveTextContent('Throw');
    act(() => (document.activeElement as HTMLElement).blur());
    expect(shown()).toBeNull();
  });

  it('leads each tile with its skill’s icon for its stage, greyed until it has a point, with the bar’s cooldown on it', () => {
    vi.spyOn(performance, 'now').mockReturnValue(1000);
    skillBar.getState().restoreTimers({ strike: { from: 0, until: 4000 } }, [], 50);
    setup();
    skills(0, { strike: 8, throw: 25 });
    const icon = (name: string) =>
      tile(name).querySelector<HTMLElement>('.skill-tile-icon .skill-icon');
    expect(icon('Strike')?.style.getPropertyValue('--icon')).toContain('lorc/sword-wound.svg');
    expect(icon('Volley')?.style.getPropertyValue('--icon')).toContain('lorc/thrown-charcoal.svg');
    expect(icon('Second Wind')?.style.getPropertyValue('--icon')).toContain('lorc/wind-hole.svg');
    expect([icon('Strike')?.className, icon('Spark')?.className]).toEqual([
      'skill-icon skill-icon-grid',
      'skill-icon skill-icon-grid skill-icon-off',
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
    expect(card('Strike')).toHaveTextContent('1.9× physical attack · 2 energy · 4.05s cooldown');
  });

  it('a granted break-free is known with no points and takes none: no +, no per-point line, no evolution, and it says it goes only while held', () => {
    setup();
    skills(3, {});
    expect(within(tile('Break Free')).queryByRole('button')).toBeNull();
    expect(tile('Break Free').querySelector('.skill-icon-off')).toBeNull();
    const free = card('Break Free');
    expect(free).toHaveTextContent('Beginner · Active · Granted');
    expect(free).toHaveTextContent(
      'Only while held: ends every hard status on you · dodging for 0.5s · 0 energy · 32s cooldown',
    );
    expect(free).not.toHaveTextContent('Each point');
    expect(free).not.toHaveTextContent('Evolves');
    expect(free).not.toHaveTextContent('once it has a point');
    expect(within(card('Roll Back')).getAllByRole('listitem')[0]).toHaveTextContent(
      'Break Free while you’re held: stunned, frozen, asleep, knocked down or pinned',
    );
    expect(plus('Strike')).toBeEnabled(); // the other rows still take points
  });

  it('a Warrior still learns the Beginner’s skills, but not its break-free: a promotion grants its own', () => {
    act(() =>
      character
        .getState()
        .applyWelcome(
          create(WelcomeSchema, { token: 't', character: { ...ayla, class: 'warrior' } }),
        ),
    );
    setup();
    expect(screen.getByRole('region', { name: 'Beginner skills' })).toBeInTheDocument();
    expect(card('Strike')).toHaveTextContent('0 points');
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Beginner',
      'Warrior',
    ]); // the Beginner first
    expect(screen.queryByRole('listitem', { name: 'Break Free' })).toBeNull();
    expect(names('Warrior skills')).toEqual([
      'Slash Blast',
      'Iron Body',
      'Toughness',
      'Unyielding',
      'Charge',
      'Ground Slam',
      'War Cry',
      'Taunt',
    ]); // by the level each opens at
    expect(card('Unyielding')).toHaveTextContent('Granted');
    expect(tile('Unyielding')).not.toHaveTextContent('Lv 10'); // granted with the class, at any level
    expect(card('Unyielding')).not.toHaveTextContent('Lv 10');
  });

  it('a root’s card lists the follow-ups its key turns into and while what, the class’s own: a promotion swaps the Beginner’s for its own', () => {
    setup();
    const items = (name: string) =>
      within(card(name))
        .getAllByRole('listitem')
        .map((li) => li.textContent);
    expect(items('Strike')).toEqual(['Lunge while the target is over 3 m away']);
    expect(card('Lunge')).toHaveTextContent(
      'Comes up on Strike’s key while the target is over 3 m away; on Roll Back’s key right after a dodge; it can’t go on a key of its own.',
    );
    act(() =>
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 't',
          character: { ...ayla, class: 'warrior', level: 20 },
        }),
      ),
    );
    skills(0, { strike: 10 });
    expect(
      within(card('Heavy Strike'))
        .getByRole('list', { name: 'Heavy Strike’s chain' })
        .querySelectorAll('li'),
    ).toHaveLength(2);
    expect(items('Heavy Strike')).toEqual([
      'Ground Slam while the target is stunned or frozen',
      'Charge while the target is over 3 m away',
    ]);
    expect(card('Charge')).toHaveTextContent(
      'Comes up on Heavy Strike’s key while the target is over 3 m away; it can’t go on a key of its own.',
    );
    expect(card('Lunge')).toHaveTextContent(
      'Comes up on Roll Back’s key right after a dodge; it can’t go on a key of its own.',
    );
    expect(card('Slash Blast').querySelector('.skill-chain')).toBeNull();
  });

  it('a follow-up that roots a chain of its own says its key is also another’s', () => {
    act(() =>
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 't',
          character: { ...ayla, class: 'magician', level: 20 },
        }),
      ),
    );
    setup();
    expect(card('Fire Arrow')).toHaveTextContent('Also on Blink’s key right after a dodge.');
    expect(card('Meteor')).toHaveTextContent(
      'Comes up on Fire Arrow’s key while the target is stunned, frozen, pinned or knocked down; on Frost Nova’s key while the target is stunned or frozen; it can’t go on a key of its own.',
    );
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
    expect(card(name)).toHaveTextContent(text);
    expect(card(name)).not.toHaveTextContent('foes');
  });

  it('+ stages points until none are left, previews the evolution, and Undo returns them', async () => {
    setup();
    skills(2, { strike: 8 });
    await user.click(plus('Strike'));
    await user.click(plus('Strike'));
    expect(plus('Spark')).toBeDisabled();
    expect(bar()).toHaveTextContent('Skill points: 0');
    expect(tile('Strike').querySelector('.skill-tile-points')).toHaveTextContent('8+2');
    expect(tile('Strike').querySelector('.skill-staged')).toHaveTextContent('+2'); // in green
    expect(card('Strike')).toHaveTextContent('Strike → Heavy Strike');
    expect(card('Strike')).toHaveTextContent('Beginner · Active · 8 points +2');
    expect(card('Strike')).toHaveTextContent(
      '1.9× physical attack · 2 energy · 4.5s cooldown · radius 2.375 → 2.7× physical attack · 2 energy · 4.5s cooldown · radius 2.875',
    );
    expect(card('Strike')).toHaveTextContent('Evolves into Crushing Blow at 25 points (15 more).');
    await user.click(within(bar()).getByRole('button', { name: 'Undo' }));
    expect(card('Strike')).not.toHaveTextContent('→');
    expect(tile('Strike').querySelector('.skill-staged')).toBeNull();
    expect(bar()).toHaveTextContent('Skill points: 2');
  });

  it('a skill with no points previews what its staged points make it', async () => {
    setup();
    skills(10, {});
    for (let i = 0; i < 3; i++) await user.click(plus('Strike'));
    expect(card('Strike')).toHaveTextContent('Usable once it has a point');
    expect(card('Strike')).toHaveTextContent(
      'With 3 points: 1.65× physical attack · 2 energy · 4.5s cooldown',
    );
    expect(card('Strike')).not.toHaveTextContent('point →');
    for (let i = 0; i < 7; i++) await user.click(plus('Strike'));
    expect(card('Strike')).toHaveTextContent('Strike → Heavy Strike');
    expect(card('Strike')).toHaveTextContent('0 points +10');
    expect(card('Strike')).toHaveTextContent(
      'With 10 points: 2.7× physical attack · 2 energy · 4.5s cooldown',
    );
    expect(card('Strike')).toHaveTextContent('A wider, harder swing.');
  });

  it('an active skill drags its ID to the skill bar, hiding its card; a passive one stays put', () => {
    setup();
    skills(0, { strike: 1 });
    card('Strike');
    const carried = dataTransfer();
    const image = vi.spyOn(carried, 'setDragImage');
    fireEvent.dragStart(tile('Strike'), { dataTransfer: carried });
    expect(carried.getData(SKILL_DRAG_TYPE)).toBe('strike');
    expect(image).toHaveBeenCalledWith(tile('Strike').querySelector('.skill-icon'), 0, 0); // its centre, 0 in jsdom
    expect(screen.queryByRole('tooltip')).toBeNull();
    expect(tile('Strike')).toHaveAttribute('draggable', 'true');
    expect(tile('Second Wind')).toHaveAttribute('draggable', 'false');
    expect(tile('Strike').querySelector('.skill-tile-chain')).toBeNull();
  });

  it('a follow-up that can’t stand on a key of its own has the chain mark and doesn’t drag; its card says why', () => {
    setup();
    skills(0, { lunge: 1 });
    for (const name of ['Lunge', 'Break Free']) {
      expect(tile(name)).toHaveAttribute('draggable', 'false');
      expect(tile(name).querySelector('.skill-tile-chain')).not.toBeNull();
      expect(card(name)).toHaveTextContent('it can’t go on a key of its own.');
      expect(card(name)).not.toHaveTextContent('Drag');
    }
    expect(tile('Roll Back').querySelector('.skill-tile-chain')).toBeNull(); // a root keys
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

  it('a skill below its level shows the level it opens at over its icon, with no +, and takes points from that level on', () => {
    const spark = skillData.find((s) => s.id === 'spark') as { level: number }; // the shipped skills all open at level 1
    const was = spark.level;
    spark.level = 6;
    try {
      setup();
      skills(3, {});
      expect(within(tile('Spark')).getByText('Lv 6')).toBeInTheDocument(); // over its icon
      expect(card('Spark')).toHaveTextContent('Lv 6: takes its first point at level 6');
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
      expect(within(tile('Spark')).queryByText('Lv 6')).not.toBeInTheDocument();
      expect(card('Spark')).not.toHaveTextContent('Lv 6');
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
