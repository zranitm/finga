import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  StatsChangedSchema,
  ExpGainedSchema,
  LevelUpSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { statuses } from '@/state/statuses';
import { ui } from '@/state/ui';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { ConnectionNotice } from './ConnectionNotice';
import { Hud } from './Hud';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 150,
  maxHp: 204,
};
const welcome = create(WelcomeSchema, { token: 'tok', character: ayla, mapId: 'town', tick: 1 });
const gained = (amount: bigint, exp: bigint) => create(ExpGainedSchema, { amount, exp });

describe('Hud', () => {
  beforeEach(() => {
    resetStores();
  });

  it('a badge counts the unspent stat points and opens the character window', async () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    expect(screen.queryByRole('button', { name: /stat point/ })).toBeNull();
    const points = (unspent: number) =>
      act(() =>
        character.getState().applyStatsChanged(
          create(StatsChangedSchema, {
            sheet: { points: { unspent } },
            hp: 150,
            maxHp: 204,
          }),
        ),
      );
    points(1);
    expect(screen.getByRole('button', { name: '1 stat point' })).toBeInTheDocument();
    // on the tray's panel under the frame, with the skill bar, so it reads on any ground
    const tray = screen.getByRole('list', { name: 'Skill bar' }).closest('.hud-tray');
    expect(tray?.contains(screen.getByRole('button', { name: '1 stat point' }))).toBe(true);
    points(5);
    await user.click(screen.getByRole('button', { name: '5 stat points' }));
    expect(ui.getState().windows).toContain('character');
    points(0);
    expect(screen.queryByRole('button', { name: /stat point/ })).toBeNull();
  });

  it('a badge counts the unspent skill points and opens the skill window', async () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    const skills = (unspent: number) =>
      act(() =>
        character.getState().applyStatsChanged(
          create(StatsChangedSchema, {
            sheet: { skills: { unspent } },
            hp: 150,
            maxHp: 204,
          }),
        ),
      );
    skills(1);
    expect(screen.getByRole('button', { name: '1 skill point' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /stat point/ })).toBeNull();
    skills(5);
    await user.click(screen.getByRole('button', { name: '5 skill points' }));
    expect(ui.getState().windows).toContain('skills');
    skills(0);
    expect(screen.queryByRole('button', { name: /skill point/ })).toBeNull();
  });

  it('shows the energy as ten pips, lit to the count shown, a press taken off at once', () => {
    character.getState().applyWelcome(welcome);
    energy.getState().welcome(6);
    render(<Hud />);
    const pips = screen.getByRole('meter', { name: 'Energy' });
    const lit = () => pips.querySelectorAll('.energy-pip-full').length;
    expect(pips).toHaveAttribute('aria-valuemax', '10');
    expect(pips).toHaveAttribute('aria-valuenow', '6');
    expect(pips.children).toHaveLength(10);
    expect(lit()).toBe(6);
    act(() => {
      energy.getState().spend(1, 2);
    });
    expect(lit()).toBe(4);
    act(() => energy.getState().delta(2, 6)); // the input after the press: an attack's 2 came, with the spend counted
    expect(lit()).toBe(6);
  });

  it('shows the player’s own statuses under their HP', () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    const own = screen.getByRole('group', { name: 'Status' });
    expect(within(own).queryByRole('list', { name: 'Buffs, statuses and cooldowns' })).toBeNull();
    const now = performance.now();
    act(() =>
      statuses.getState().show({
        me: [{ id: 'stun', from: now, until: now + 1000 }],
        target: [],
        party: {},
      }),
    );
    const list = within(own).getByRole('list', { name: 'Buffs, statuses and cooldowns' });
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((li) => li.title),
    ).toEqual(['Stun']);
  });

  it('shows walk mode first under the player’s HP while it’s on (large-regions.md section 7)', () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    const own = screen.getByRole('group', { name: 'Status' });
    expect(within(own).queryByRole('listitem', { name: 'Walk mode' })).toBeNull();
    act(() => settings.getState().setWalk(true));
    expect(within(own).getByRole('listitem', { name: 'Walk mode' })).toBeInTheDocument();
    act(() => settings.getState().setWalk(false));
    expect(within(own).queryByRole('listitem', { name: 'Walk mode' })).toBeNull();
  });

  it('shows the player’s buffs from the sheet and a follow-up cooling off the keys under their HP, until a key offers it (C22)', () => {
    character.getState().applyWelcome(welcome);
    act(() => {
      skillBar.getState().load(['strike']);
      character.getState().applyStatsChanged(
        create(StatsChangedSchema, {
          hp: 150,
          maxHp: 204,
          sheet: {
            skills: { invested: { strike: 1, ground_slam: 1 } },
            buffs: [{ skill: 'iron_body', leftMs: 20000n, points: 1 }],
          },
        }),
      );
    });
    render(<Hud />);
    const own = screen.getByRole('group', { name: 'Status' });
    const row = () =>
      within(own)
        .queryAllByRole('listitem')
        .map((li) => li.getAttribute('aria-label'));
    expect(row()).toEqual(['Iron Body']);
    const now = performance.now();
    act(() => skillBar.getState().cool('ground_slam', { from: now, until: now + 8000 }));
    expect(
      within(own).getByRole('list', { name: 'Buffs, statuses and cooldowns' }),
    ).toBeInTheDocument();
    expect(row()).toEqual(['Iron Body', 'Ground Slam, cooling']);
    act(() => skillBar.getState().showLive(['ground_slam']));
    expect(row()).toEqual(['Iron Body']);
  });

  it('shows nothing before a character arrives', () => {
    render(<Hud />);
    expect(screen.queryByLabelText('Status')).not.toBeInTheDocument();
  });

  it('renders the bars from the store and updates on level up and EXP gained', () => {
    character.getState().applyWelcome(welcome);
    render(<Hud />);
    const frame = screen.getByRole('group', { name: 'Status' });
    expect(frame).toHaveClass('unit-frame', 'side-ally');
    expect(frame.querySelector('.side-mark circle')).not.toBeNull(); // the ally's dot
    expect(within(frame).getByRole('meter', { name: 'EXP' })).toBeInTheDocument(); // under the HP bar, in the frame
    expect(screen.getByText('Ayla')).toBeInTheDocument();
    expect(screen.getByText('Lv. 1')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '150');
    const exp = screen.getByRole('meter', { name: 'EXP' });
    expect(exp).toHaveAttribute('aria-valuenow', '10');
    expect(exp).toHaveAttribute('aria-valuemax', '20');

    act(() => character.getState().applyExpGained(gained(5n, 15n)));
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuenow', '15');

    act(() =>
      character.getState().applyLevelUp(
        create(LevelUpSchema, {
          character: { ...ayla, level: 2, exp: 0n, hp: 230, maxHp: 230 },
        }),
      ),
    );
    expect(screen.getByText('Lv. 2')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '230');
    expect(screen.getByRole('meter', { name: 'EXP' })).toHaveAttribute('aria-valuemax', '65');

    act(() => character.getState().setHp(100));
    expect(screen.getByRole('meter', { name: 'HP' })).toHaveAttribute('aria-valuenow', '100');
  });

  it('shows a shield pale over the HP bar from its fill’s end, never past the bar’s, its amount after the numbers; gone once spent', () => {
    character.getState().applyWelcome(welcome); // 150 of 204
    render(<Hud />);
    const hp = screen.getByRole('meter', { name: 'HP' });
    const part = () => hp.querySelector<HTMLElement>('.bar-shield');
    expect(part()).toBeNull();
    act(() => character.getState().setShield(30));
    expect([part()?.style.left, part()?.style.width]).toEqual(['73.5%', '14.7%']);
    expect(hp).toHaveTextContent('150 / 204 +30');
    act(() => character.getState().setShield(100)); // past the bar's end: it ends there, over the fill
    expect([part()?.style.left, part()?.style.width]).toEqual(['51%', '49%']);
    act(() => character.getState().setShield(0));
    expect([part(), hp.textContent?.includes('+')]).toEqual([null, false]);
    act(() => character.getState().setShield(30));
    act(() => character.getState().applyWelcome(welcome)); // a new map or a rejoin starts with the frames' own
    expect(part()).toBeNull();
  });

  it('at the level cap the EXP bar is full and says MAX', () => {
    character
      .getState()
      .applyWelcome(create(WelcomeSchema, { character: { ...ayla, level: 30, exp: 0n } }));
    render(<Hud />);
    const exp = screen.getByRole('meter', { name: 'EXP' });
    expect(exp).toHaveAttribute('aria-valuenow', '1');
    expect(exp).toHaveAttribute('aria-valuemax', '1');
    expect(exp).toHaveTextContent('MAX');
  });
});

describe('ConnectionNotice', () => {
  beforeEach(() => {
    resetStores();
  });

  it.each([
    {
      name: 'disconnected',
      set: () => connection.getState().setConnection('disconnected'),
      shown: false,
    },
    {
      name: 'connecting',
      set: () => connection.getState().setConnection('connecting'),
      shown: false,
    },
    {
      name: 'connected',
      set: () => connection.getState().setConnection('connected'),
      shown: false,
    },
    {
      name: 'reconnecting',
      set: () => connection.getState().setConnection('reconnecting', { attempt: 1 }),
      shown: true,
    },
  ])('$name shows the notice: $shown', ({ set, shown }) => {
    set();
    render(<ConnectionNotice />);
    expect(screen.queryByRole('status') !== null).toBe(shown);
  });

  it('shows on reconnecting with the attempt, and hides on connected', () => {
    render(<ConnectionNotice />);
    act(() => connection.getState().setConnection('reconnecting', { attempt: 1 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting…');
    act(() => connection.getState().setConnection('reconnecting', { attempt: 3 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting (attempt 3)…');
    act(() => connection.getState().setConnection('connected'));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('a stalled world on an open socket says it waits for the server; a reconnect says so instead', () => {
    act(() => connection.getState().setConnection('connected'));
    render(<ConnectionNotice />);
    act(() => connection.getState().setWorldStalled(true));
    expect(screen.getByRole('status')).toHaveTextContent('Waiting for the server…');
    act(() => connection.getState().setConnection('reconnecting', { attempt: 1 }));
    expect(screen.getByRole('status')).toHaveTextContent('Reconnecting…');
    act(() => connection.getState().setConnection('connected'));
    act(() => connection.getState().setWorldStalled(false));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });
});
