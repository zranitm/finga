import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { PartyPanel } from '@/features/hud/PartyPanel';
import { MatchPhase, MatchStateSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { fighters } from '@/state/fighters';
import type { FighterLive } from '@/state/fighters';
import { pvp } from '@/state/pvp';
import { resetStores } from '@/testing/reset-stores';

import { EnemyFrames, TeamFrames } from './MatchFrames';

/** Ayla's match on side 2 with Robin and the bot Kit, against Ash, Bea and the bot Cole. */
const match = (mode: string) =>
  act(() =>
    pvp.getState().applyMatchState(
      create(MatchStateSchema, {
        match: 'mt-1',
        mode,
        phase: MatchPhase.FIGHT,
        round: 1,
        sides: [
          {
            fighters: [
              { id: 'c-ash', name: 'Ash', class: 'thief' },
              { id: 'c-bea', name: 'Bea', class: 'bowman' },
              { id: 'b-cole', name: 'Cole', class: 'magician', bot: true },
            ],
          },
          {
            fighters: [
              { id: 'c-ayla', name: 'Ayla', class: 'warrior' },
              { id: 'c-robin', name: 'Robin', class: 'magician' },
              { id: 'b-kit', name: 'Kit', class: 'bowman', bot: true },
            ],
          },
        ],
      }),
      0,
    ),
  );

const live = (hp: number, more: Partial<FighterLive> = {}): FighterLive => ({
  level: 30,
  hp,
  maxHp: 100,
  down: false,
  statuses: [],
  ...more,
});

const show = (next: Record<string, FighterLive>) => act(() => fighters.getState().show(next));

describe('match frames', () => {
  beforeEach(() => {
    resetStores();
    character
      .getState()
      .applyWelcome(
        create(WelcomeSchema, { character: { id: 'c-ayla', name: 'Ayla', class: 'warrior' } }),
      );
  });

  it('in 3v3 show the teammates in the party frames’ place and the enemies under the bar, each with its HP, statuses and BOT tag, dimmed while down', () => {
    render(
      <>
        <PartyPanel />
        <TeamFrames />
        <EnemyFrames />
      </>,
    );
    match('team');
    const now = performance.now();
    show({
      'c-ayla': live(100),
      'c-robin': live(60, { statuses: [{ id: 'slow', from: now, until: now + 2000 }] }),
      'b-kit': live(0, { down: true }),
      'c-ash': live(40),
      'c-bea': live(100),
      'b-cole': live(80),
    });
    const team = screen.getByRole('group', { name: 'Teammates' });
    expect(
      within(team)
        .getAllByRole('group')
        .map((g) => g.getAttribute('aria-label')),
    ).toEqual(['Teammate Robin', 'Teammate Kit']); // the player has their own frame
    expect(screen.getByRole('meter', { name: 'Robin HP' })).toHaveAttribute('aria-valuenow', '60');
    const robin = screen.getByRole('group', { name: 'Teammate Robin' });
    expect(
      within(within(robin).getByRole('list', { name: 'Statuses' }))
        .getAllByRole('listitem')
        .map((li) => li.title),
    ).toEqual(['Slow']);
    const kit = screen.getByRole('group', { name: 'Teammate Kit' });
    expect(kit).toHaveTextContent('BOT');
    expect(kit).toHaveTextContent('Down');
    expect(kit).toHaveClass('unit-frame-dimmed');
    const foes = screen.getByRole('group', { name: 'Enemies' });
    expect(
      within(foes)
        .getAllByRole('group')
        .map((g) => g.getAttribute('aria-label')),
    ).toEqual(['Enemy Ash', 'Enemy Bea', 'Enemy Cole']);
    expect(screen.getByRole('meter', { name: 'Ash HP' })).toHaveAttribute('aria-valuenow', '40');
    expect(screen.getByRole('group', { name: 'Enemy Cole' })).toHaveTextContent('BOT');
  });

  it('show a fighter once the world has drawn them, and none outside a match', () => {
    render(
      <>
        <TeamFrames />
        <EnemyFrames />
      </>,
    );
    show({ 'c-robin': live(60) });
    expect(screen.queryByRole('group', { name: 'Teammates' })).not.toBeInTheDocument();
    match('team');
    expect(screen.getByRole('group', { name: 'Teammates' })).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Teammate Kit' })).not.toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Enemies' })).not.toBeInTheDocument();
    show({ 'c-robin': live(60), 'c-ash': live(100) });
    expect(screen.getByRole('group', { name: 'Enemy Ash' })).toBeInTheDocument();
  });

  it('leave a battle, and a mode this client doesn’t know, to the party frames, and frame no enemies in a battle', () => {
    for (const mode of ['battle', 'siege']) {
      const { unmount } = render(
        <>
          <TeamFrames />
          <EnemyFrames />
        </>,
      );
      match(mode);
      show({ 'c-robin': live(60), 'c-ash': live(80) });
      expect(screen.queryByRole('group', { name: 'Teammates' })).not.toBeInTheDocument();
      expect(screen.queryByRole('group', { name: 'Enemies' })).not.toBeInTheDocument();
      unmount();
    }
  });

  it('in 1v1 show none: the bar names the enemy, and there are no teammates', () => {
    render(
      <>
        <TeamFrames />
        <EnemyFrames />
      </>,
    );
    act(() =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'duel',
          phase: MatchPhase.FIGHT,
          round: 1,
          sides: [
            { fighters: [{ id: 'c-ash', name: 'Ash', class: 'thief' }] },
            { fighters: [{ id: 'c-ayla', name: 'Ayla', class: 'warrior' }] },
          ],
        }),
        0,
      ),
    );
    show({ 'c-ash': live(100) });
    expect(screen.queryByRole('group', { name: 'Enemies' })).not.toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Teammates' })).not.toBeInTheDocument();
  });
});

describe('fighters store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('keeps a fighter the same as before, so their frame doesn’t render again, and drops one no longer drawn', () => {
    fighters.getState().show({ 'c-ash': live(40), 'c-bea': live(100) });
    const was = fighters.getState().live;
    fighters.getState().show({ 'c-ash': live(40), 'c-bea': live(100) });
    expect(fighters.getState().live).toBe(was);
    fighters.getState().show({ 'c-ash': live(30), 'c-bea': live(100) });
    const now = fighters.getState().live;
    expect(now['c-bea']).toBe(was['c-bea']);
    expect(now['c-ash']!.hp).toBe(30);
    fighters.getState().show({ 'c-ash': live(30) });
    expect(Object.keys(fighters.getState().live)).toEqual(['c-ash']);
  });
});
