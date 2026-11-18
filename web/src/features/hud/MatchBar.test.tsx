import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { MatchPhase, MatchStateSchema, WelcomeSchema } from '@/protocol/messages';
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { MatchBar } from './MatchBar';

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

  it('counts into a round in big numbers over the world', () => {
    render(<MatchBar socket={fakeSocket().socket} />);
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
  });
});
