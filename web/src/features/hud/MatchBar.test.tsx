import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { MatchPhase, MatchStateSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { MatchBar, MatchCount } from './MatchBar';

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

/** Ayla's 1v1 against the bot Ash, Ayla on side 2. */
const state = (phase: MatchPhase, round: number, wins: number[], endsInMs: number) =>
  act(() =>
    pvp.getState().applyMatchState(
      create(MatchStateSchema, {
        match: 'mt-1',
        mode: 'duel',
        phase,
        round,
        wins,
        endsInMs,
        sides: [
          { fighters: [{ id: 'b-1', name: 'Ash', class: 'thief', bot: true }] },
          { fighters: [{ id: 'c-ayla', name: 'Ayla', class: 'warrior' }] },
        ],
      }),
      0,
    ),
  );

const settle = () => act(() => Promise.resolve());

describe('MatchBar', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
    character
      .getState()
      .applyWelcome(
        create(WelcomeSchema, { character: { id: 'c-ayla', name: 'Ayla', class: 'warrior' } }),
      );
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows each side’s rounds won from the player’s side, the phase and its time counting down, and the enemy with its BOT tag', () => {
    render(<MatchBar socket={fakeSocket().socket} />);
    expect(screen.queryByRole('group', { name: 'Match' })).not.toBeInTheDocument();
    state(MatchPhase.PREP, 1, [0, 0], 10_000);
    const bar = screen.getByRole('group', { name: 'Match' });
    const phase = () => [
      bar.querySelector('.match-phase')?.textContent,
      within(bar).getByTitle('Time left').textContent,
    ];
    expect(phase()).toEqual(['Get ready', '0:10']);
    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(phase()).toEqual(['Get ready', '0:06']);
    expect(within(bar).getByRole('img', { name: 'Your rounds: 0 of 2' })).toBeInTheDocument();
    expect(bar.querySelector('.side-enemy')).toHaveTextContent('AshBOT');
    state(MatchPhase.FIGHT, 2, [0, 1], 60_000);
    expect(phase()).toEqual(['Round 2', '1:00']);
    expect(
      within(bar).getByRole('img', { name: 'Your rounds: 1 of 2' }).querySelectorAll('.won'),
    ).toHaveLength(1);
    expect(within(bar).getByRole('img', { name: 'Their rounds: 0 of 2' })).toBeInTheDocument();
    state(MatchPhase.BETWEEN, 2, [1, 1], 5000);
    expect(phase()).toEqual(['Next round in', '0:05']);
    state(MatchPhase.RESULT, 3, [1, 2], 10_000);
    expect(phase()).toEqual(['Back in', '0:10']);
    expect(within(bar).queryByRole('button', { name: 'Leave match' })).not.toBeInTheDocument(); // it's over
  });

  it('in 3v3 leaves the enemies’ names to their frames under it', () => {
    render(<MatchBar socket={fakeSocket().socket} />);
    act(() =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'team',
          phase: MatchPhase.FIGHT,
          round: 1,
          sides: [
            {
              fighters: [
                { id: 'c-ayla', name: 'Ayla', class: 'warrior' },
                { id: 'c-robin', name: 'Robin', class: 'magician' },
              ],
            },
            {
              fighters: [
                { id: 'c-ash', name: 'Ash', class: 'thief' },
                { id: 'b-1', name: 'Cole', class: 'bowman', bot: true },
              ],
            },
          ],
        }),
        0,
      ),
    );
    const bar = screen.getByRole('group', { name: 'Match' });
    expect(within(bar).getByRole('img', { name: 'Their rounds: 0 of 2' })).toBeInTheDocument();
    expect(bar.querySelector('.side-enemy')).not.toHaveTextContent(/Ash|Cole/);
  });

  it('in a battle shows both scores from the player’s side and each point in its holder’s colour, its ring turned toward a side, and contested; no rounds', () => {
    render(<MatchBar socket={fakeSocket().socket} />);
    act(() =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'battle',
          phase: MatchPhase.FIGHT,
          endsInMs: 600_000,
          score: [290, 312],
          points: [
            { id: 'west', holder: 2, progress: 1 },
            { id: 'middle', holder: 0, progress: -0.5, contested: true },
            { id: 'east', holder: 1, progress: -1 },
          ],
          sides: [
            { fighters: [{ id: 'c-ash', name: 'Ash', class: 'thief' }] },
            { fighters: [{ id: 'c-ayla', name: 'Ayla', class: 'warrior' }] },
          ],
        }),
        0,
      ),
    );
    const bar = screen.getByRole('group', { name: 'Match' });
    expect(within(bar).getByLabelText('Your score')).toHaveTextContent('312');
    expect(within(bar).getByLabelText('Their score')).toHaveTextContent('290');
    expect(bar).toHaveTextContent('Battle');
    expect(bar).not.toHaveTextContent('Round');
    expect(within(bar).queryByRole('img', { name: /rounds/ })).not.toBeInTheDocument();
    expect(bar.querySelector('.side-enemy')).not.toHaveTextContent('Ash');
    const points = within(within(bar).getByRole('list', { name: 'Points' })).getAllByRole(
      'listitem',
    );
    expect(points.map((p) => [p.getAttribute('aria-label'), p.className, p.textContent])).toEqual([
      ['West Ruin: yours', 'match-point point-ally', 'West'],
      ['Middle Ruin: no one’s, contested', 'match-point point-none contested', 'Middle'],
      ['East Ruin: theirs', 'match-point point-enemy', 'East'],
    ]);
    const ring = (k: number) => points[k]!.querySelector('.match-point-ring')!;
    const length = 2 * Math.PI * 9;
    expect(ring(0)).toHaveClass('point-ally');
    expect(ring(0).getAttribute('stroke-dasharray')).toBe(`${length} ${length}`);
    expect(ring(1)).toHaveClass('point-enemy'); // turning toward side 1, theirs
    expect(ring(1).getAttribute('stroke-dasharray')).toBe(`${0.5 * length} ${length}`);
    expect(ring(2)).toHaveClass('point-enemy');
  });

  it('counts into a round in big numbers over the world', () => {
    render(
      <>
        <MatchBar socket={fakeSocket().socket} />
        <MatchCount />
      </>,
    );
    state(MatchPhase.COUNTDOWN, 1, [0, 0], 3000);
    const count = () => document.querySelector('.match-count')?.textContent;
    expect([screen.getByRole('group').querySelector('.match-phase')?.textContent, count()]).toEqual(
      ['Round 1 starts in', '3'],
    );
    act(() => {
      vi.advanceTimersByTime(1250);
    });
    expect(count()).toBe('2');
    state(MatchPhase.FIGHT, 1, [0, 0], 60_000);
    expect(count()).toBeUndefined();
  });

  it('Leave match asks first: Stay keeps the match, Leave forfeits it, and a refusal shows', async () => {
    const { socket, calls } = fakeSocket([refusal(ERR.PVP_NOT_IN_MATCH)]);
    render(<MatchBar socket={socket} />);
    state(MatchPhase.FIGHT, 1, [0, 0], 60_000);
    act(() => screen.getByRole('button', { name: 'Leave match' }).click());
    expect(screen.getByRole('group')).toHaveTextContent('Leave and lose?');
    act(() => screen.getByRole('button', { name: 'Stay' }).click());
    expect(calls).toEqual([]);
    act(() => screen.getByRole('button', { name: 'Leave match' }).click());
    act(() => screen.getByRole('button', { name: 'Leave' }).click());
    await settle();
    expect(calls).toEqual([{ t: 'pvpForfeit', d: {} }]);
    expect(screen.getByRole('alert')).toHaveTextContent('You’re not in a match.');
    act(() => screen.getByRole('button', { name: 'Leave match' }).click());
    expect(screen.queryByRole('alert')).not.toBeInTheDocument(); // a new try starts afresh
  });
});
