import { create } from '@bufbuild/protobuf';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { skills as skillData } from '@/gamedata/skills';
import { RequestError } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import { ResponseSchema, StatsChangedSchema, WelcomeSchema } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import { character } from '@/state/character';
import { combat } from '@/state/combat';
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

/** A StatsChanged with these skill points and traits, the rest of the sheet left out. */
function skills(
  unspent: number,
  invested: Record<string, number>,
  traits: Record<string, string> = {},
) {
  act(() =>
    character.getState().applyStatsChanged(
      create(StatsChangedSchema, {
        sheet: { skills: { unspent, invested, traits } },
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
  const { unmount } = render(<SkillWindow socket={{ request } as never} onClose={onClose} />);
  return { calls, onClose, unmount };
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
      'Guard',
      'Lunge',
      'Roll Back',
    ]);
    expect(tile('Strike')).toHaveTextContent('8Strike');
    expect(tile('Spark')).toHaveTextContent('0Spark');
    expect(tile('Break Free')).toHaveTextContent(/^Break Free$/); // granted: no points to show
    expect(tile('Guard')).toHaveTextContent(/^Guard$/); // C21's block, granted as well
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
    expect(
      within(
        within(card('Roll Back')).getByRole('list', { name: 'Roll Back’s chain' }),
      ).getAllByRole('listitem')[0],
    ).toHaveTextContent(
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
      'Chains: what each key turns into ▾',
    ]); // the Beginner first, the chains panel under the classes
    expect(screen.queryByRole('listitem', { name: 'Break Free' })).toBeNull();
    expect(names('Warrior skills')).toEqual([
      'Slash Blast',
      'Iron Body',
      'Toughness',
      'Unyielding',
      'Bastion',
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
      within(within(card(name)).getByRole('list', { name: `${name}’s chain` }))
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

describe('traits', () => {
  beforeEach(() => {
    resetStores();
    combat.getState().clear();
    character.getState().applyWelcome(create(WelcomeSchema, { token: 't', character: ayla }));
  });

  const traits = (name: string) => screen.getByRole('group', { name: `${name}’s traits` });
  const diamond = (skill: string, trait: string) =>
    within(traits(skill)).getByRole('button', { name: new RegExp(`^${trait}`) });
  /** The card of a trait, shown by hovering its diamond. */
  const traitCard = (skill: string, trait: string) => {
    fireEvent.mouseEnter(diamond(skill, trait), { clientX: 100, clientY: 50 });
    fireEvent.mouseMove(diamond(skill, trait), { clientX: 101, clientY: 50 });
    return screen.getByRole('tooltip');
  };

  it('puts two diamonds under each active tile with traits, the chosen one filled, and none under a passive', () => {
    setup();
    skills(0, { strike: 25, throw: 3, second_wind: 4 }, { strike: 'concussion' });
    expect(diamond('Crushing Blow', 'Concussion')).toHaveAttribute('aria-pressed', 'true');
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'false');
    expect(
      diamond('Crushing Blow', 'Concussion').querySelector('.skill-trait-fill'),
    ).not.toBeNull();
    expect(diamond('Throw', 'Bola')).toHaveAccessibleName(/^Bola, not open yet$/);
    expect(diamond('Throw', 'Bola').querySelector('.skill-trait-lock')).not.toBeNull();
    expect(screen.queryByRole('group', { name: 'Second Wind’s traits' })).toBeNull();
    expect(diamond('Spark', 'Crackle')).toHaveAttribute('aria-disabled', 'false'); // chosen, so nothing to send
    expect(diamond('Spark', 'Static')).toHaveAttribute('aria-disabled', 'true'); // no points: nothing to choose for
  });

  it('a stored trait the skill lacks, or an extra not open, shows the default chosen, as the world uses it', () => {
    setup();
    skills(0, { strike: 25 }, { strike: 'no_such_trait' });
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'true');
    skills(0, { strike: 0 }, { strike: 'concussion' }); // its points reset, the choice kept
    expect(diamond('Strike', 'Swift')).toHaveAttribute('aria-pressed', 'true');
    expect(diamond('Strike', 'Concussion')).toHaveAttribute('aria-pressed', 'false');
    expect(card('Strike')).toHaveTextContent('◆ Swift (chosen)');
  });

  it('a click on the other sends the choice at once, and it pulses until the world answers and the sheet has it', async () => {
    let answer!: (r: Data<'response'>) => void;
    const { calls } = setup([new Promise<Data<'response'>>((r) => (answer = r)) as never]);
    skills(0, { strike: 25, throw: 25 });
    await user.click(diamond('Crushing Blow', 'Concussion'));
    expect(calls).toEqual([{ t: 'chooseTrait', d: { skill: 'strike', trait: 'concussion' } }]);
    expect(diamond('Crushing Blow', 'Concussion')).toHaveClass('skill-trait-pending');
    expect(diamond('Volley', 'Bola')).toHaveAttribute('aria-disabled', 'true');
    await user.click(diamond('Volley', 'Bola'));
    expect(calls).toHaveLength(1); // one choice at a time
    answer(ok);
    await act(() => Promise.resolve());
    expect(diamond('Crushing Blow', 'Concussion')).toHaveClass('skill-trait-pending'); // the world agreed; its sheet is a tick behind
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'true');
    skills(0, { strike: 25, throw: 25 }, { strike: 'concussion' });
    expect(diamond('Crushing Blow', 'Concussion')).not.toHaveClass('skill-trait-pending');
    expect(diamond('Crushing Blow', 'Concussion')).toHaveAttribute('aria-pressed', 'true');
    expect(diamond('Volley', 'Bola')).toHaveAttribute('aria-disabled', 'false');
  });

  it('a sheet after the world agreed ends the pulse even without the change, and shows what the sheet holds', async () => {
    setup();
    skills(0, { strike: 25 });
    await user.click(diamond('Crushing Blow', 'Concussion'));
    expect(diamond('Crushing Blow', 'Concussion')).toHaveClass('skill-trait-pending');
    skills(0, { strike: 25 });
    expect(diamond('Crushing Blow', 'Concussion')).not.toHaveClass('skill-trait-pending');
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'true');
  });

  it('a sheet ahead of the answer ends the pulse with it, and a click on the chosen one sends nothing', async () => {
    let answer!: (r: Data<'response'>) => void;
    const { calls } = setup([new Promise<Data<'response'>>((r) => (answer = r)) as never]);
    skills(0, { strike: 25 }, { strike: 'concussion' });
    await user.click(diamond('Crushing Blow', 'Concussion'));
    expect(calls).toEqual([]);
    await user.click(diamond('Crushing Blow', 'Swift'));
    skills(0, { strike: 25 });
    expect(diamond('Crushing Blow', 'Swift')).not.toHaveClass('skill-trait-pending');
    answer(ok);
    await act(() => Promise.resolve());
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'true');
  });

  it('a refusal leaves the chosen one and shows the world’s words; no answer asks to try again', async () => {
    setup([refusal(ERR.IN_COMBAT), new RequestError('timeout', 'no response')]);
    skills(0, { strike: 25 });
    await user.click(diamond('Crushing Blow', 'Concussion'));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'You can’t change traits in combat.',
    );
    expect(diamond('Crushing Blow', 'Swift')).toHaveAttribute('aria-pressed', 'true');
    expect(diamond('Crushing Blow', 'Concussion')).not.toHaveClass('skill-trait-pending');
    await user.click(diamond('Crushing Blow', 'Concussion'));
    expect(await screen.findByRole('alert')).toHaveTextContent('No answer yet. Try again.');
  });

  it('a shut extra sends nothing: staged points don’t open it, applied ones do', async () => {
    const { calls } = setup();
    skills(1, { strike: 24 });
    await user.click(plus('Heavy Strike'));
    await user.click(diamond('Heavy Strike', 'Concussion'));
    expect(diamond('Heavy Strike', 'Concussion')).toHaveAttribute('aria-disabled', 'true');
    expect(calls).toEqual([]);
    skills(0, { strike: 25 });
    expect(diamond('Crushing Blow', 'Concussion')).toHaveAttribute('aria-disabled', 'false');
  });

  it('in combat the diamonds dim and send nothing, and the header says why, until the fight is past', () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { calls } = setup();
      skills(0, { strike: 25 });
      act(() => combat.getState().fought());
      expect(screen.getByText('In combat: traits change once out of it.')).toBeInTheDocument();
      expect(traits('Crushing Blow')).toHaveClass('skill-traits-fighting');
      fireEvent.click(diamond('Crushing Blow', 'Concussion'));
      expect(calls).toEqual([]);
      act(() => {
        vi.advanceTimersByTime(4999);
      });
      expect(traits('Crushing Blow')).toHaveClass('skill-traits-fighting');
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByText('In combat: traits change once out of it.')).toBeNull();
      fireEvent.click(diamond('Crushing Blow', 'Concussion'));
      expect(calls).toEqual([{ t: 'chooseTrait', d: { skill: 'strike', trait: 'concussion' } }]);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a diamond’s card tells the trait, what the skill does with it at the last stage, and what a click does', () => {
    setup();
    skills(0, { strike: 12, throw: 25 }, { throw: 'bola' });
    const extra = traitCard('Heavy Strike', 'Concussion');
    expect(extra).toHaveTextContent('ConcussionExtra · Crushing Blow');
    expect(extra).toHaveTextContent('Stuns what it hits for 1 s; ready again in 7 s.');
    expect(extra).toHaveTextContent('With 25 points: 4.25× physical attack');
    expect(extra).toHaveTextContent('7s cooldown');
    expect(extra).toHaveTextContent('Opens at 25 points, as Crushing Blow');
    const swift = traitCard('Heavy Strike', 'Swift');
    expect(swift).toHaveTextContent('SwiftDefault · Crushing Blow');
    expect(swift).toHaveTextContent('With 25 points: 4.25× physical attack');
    expect(swift).toHaveTextContent('4.5s cooldown');
    expect(swift).toHaveTextContent('Chosen');
    expect(traitCard('Volley', 'Bola')).toHaveTextContent('Chosen');
    expect(traitCard('Volley', 'Stone')).toHaveTextContent('Click to choose');
    act(() => combat.getState().fought());
    expect(traitCard('Volley', 'Stone')).toHaveTextContent(
      'In combat: traits change once out of it.',
    );
    fireEvent.mouseLeave(diamond('Volley', 'Stone'));
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('a skill’s card lists both traits, the chosen one marked, and says what it does with it', () => {
    setup();
    skills(0, { strike: 25 }, { strike: 'concussion' });
    const strike = card('Crushing Blow');
    expect(within(strike).getByRole('list', { name: 'Traits' })).toHaveTextContent(
      '◇ Swift: Ready again in 4.5 s.◆ Concussion (chosen): Stuns what it hits for 1 s; ready again in 7 s.',
    );
    expect(strike).toHaveTextContent('7s cooldown');
  });
});

describe('the chains panel', () => {
  const as = (cls: string, level = 20) =>
    act(() =>
      character
        .getState()
        .applyWelcome(
          create(WelcomeSchema, { token: 't', character: { ...ayla, class: cls, level } }),
        ),
    );
  const panel = () => screen.getByRole('region', { name: 'Chains' });
  const chains = () =>
    within(panel())
      .queryAllByRole('listitem')
      .filter((li) => li.classList.contains('skill-chain-row'));
  const chain = (name: string) => chains().find((li) => li.getAttribute('aria-label') === name)!;
  const steps = (name: string) =>
    within(chain(name))
      .getAllByRole('listitem')
      .map((li) => li.textContent);

  beforeEach(() => {
    resetStores();
    localStorage.removeItem('finga.skills.chains');
  });

  it('lists the class’s chains in order under the grid, each root then a line per follow-up with its condition in a few words', () => {
    as('warrior');
    setup();
    skills(0, { strike: 10, war_cry: 1, iron_body: 1 });
    expect(chains().map((li) => li.getAttribute('aria-label'))).toEqual([
      'Heavy Strike’s chain',
      'Roll Back’s chain',
      'War Cry’s chain',
      'Bastion’s chain',
    ]);
    expect(steps('Heavy Strike’s chain')).toEqual([
      '─stunned or frozen▶Ground Slam',
      '└over 3 m▶Charge',
    ]);
    expect(steps('Roll Back’s chain')).toEqual(['─while held▶Unyielding', '└after a dodge▶Lunge']);
    expect(steps('War Cry’s chain')).toEqual(['─right after▶Iron Body']);
    expect(steps('Bastion’s chain')).toEqual(['─after a block▶Ground Slam']); // C21's counter
    expect(within(chain('Heavy Strike’s chain')).getByText('stunned or frozen')).toHaveAttribute(
      'title',
      'while the target is stunned or frozen',
    ); // in full on hover
    expect(within(chain('War Cry’s chain')).getByText('right after')).toHaveAttribute(
      'title',
      'right after War Cry',
    );
  });

  it('shows a skill in two chains in each, and a root that’s also a follow-up as both', () => {
    as('magician');
    setup();
    const shown = (name: string) => within(panel()).getAllByRole('img', { name }).length;
    expect([shown('Meteor'), shown('Fire Arrow')]).toEqual([2, 2]);
    expect(steps('Blink’s chain')).toEqual([
      '─while held▶Arcane Release',
      '└after a dodge▶Fire Arrow',
    ]);
  });

  it('greys an icon with no points, and shows the skill’s card from it, the icon under the pointer the one described', () => {
    as('warrior');
    setup();
    skills(0, { strike: 10 });
    const icon = (name: string) => within(panel()).getAllByRole('img', { name })[0]!;
    expect(icon('Ground Slam').querySelector('.skill-icon-off')).not.toBeNull();
    expect(icon('Heavy Strike').querySelector('.skill-icon-off')).toBeNull();
    fireEvent.mouseEnter(icon('Heavy Strike'), { clientX: 40, clientY: 400 });
    const card = screen.getByRole('tooltip');
    expect(card).toHaveTextContent('Heavy Strike');
    expect(icon('Heavy Strike')).toHaveAttribute('aria-describedby', card.id);
    expect(tile('Heavy Strike')).not.toHaveAttribute('aria-describedby'); // the same skill's tile isn't
    fireEvent.mouseLeave(icon('Heavy Strike'));
    expect(screen.queryByRole('tooltip')).toBeNull();
    act(() => icon('Charge').focus());
    expect(screen.getByRole('tooltip')).toHaveTextContent('it can’t go on a key of its own');
  });

  it('a root drags onto the bar as its tile does; a follow-up doesn’t, even one that roots elsewhere', () => {
    as('magician');
    setup();
    const [root, follow] = within(panel()).getAllByRole('img', { name: 'Fire Arrow' });
    expect([root, follow].map((e) => e!.getAttribute('draggable'))).toEqual(['true', 'false']);
    const carried = dataTransfer();
    fireEvent.dragStart(root!, { dataTransfer: carried });
    expect(carried.getData(SKILL_DRAG_TYPE)).toBe('fire_arrow');
  });

  it('folds away from its header and back, kept per browser; it starts open', async () => {
    as('warrior');
    const { unmount } = setup();
    const fold = () =>
      within(panel()).getByRole('button', { name: /Chains: what each key turns into/ });
    expect(fold()).toHaveAttribute('aria-expanded', 'true');
    await user.click(fold());
    expect(fold()).toHaveAttribute('aria-expanded', 'false');
    expect(chains()).toHaveLength(0);
    expect(localStorage.getItem('finga.skills.chains')).toBe('closed');
    unmount();
    setup();
    expect(fold()).toHaveAttribute('aria-expanded', 'false'); // as it was left
    await user.click(fold());
    expect(chains()).toHaveLength(4);
    expect(localStorage.getItem('finga.skills.chains')).toBe('open');
  });

  it('starts open when storage can’t be read', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    try {
      as('warrior');
      setup();
      expect(chains()).toHaveLength(4);
    } finally {
      vi.restoreAllMocks();
    }
  });
});
