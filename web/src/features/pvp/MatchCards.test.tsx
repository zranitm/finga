import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import {
  MatchEndedSchema,
  MatchOutcome,
  MatchPhase,
  MatchStateSchema,
  PvpOfferSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { MatchOffer } from './MatchOffer';
import { MatchResult } from './MatchResult';

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

const offer = (id = 'of-1', bots = 1) =>
  act(() =>
    pvp
      .getState()
      .applyOffer(create(PvpOfferSchema, { offer: id, mode: 'duel', bots, untilMs: 10_000 })),
  );

/** Lets the request's promise settle, as the fake timers hold the clock still. */
const settle = () => act(() => Promise.resolve());

describe('MatchOffer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('says the mode and its bots, counts down, and Accept sends the answer and waits for the others', async () => {
    const { socket, calls } = fakeSocket();
    render(<MatchOffer socket={socket} />);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    offer();
    const card = screen.getByRole('alertdialog', { name: 'Match found' });
    expect(card).toHaveTextContent('1v1 · 1 bot');
    expect(within(card).getByLabelText('10 seconds left')).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(3500);
    });
    expect(within(card).getByLabelText('7 seconds left')).toBeInTheDocument();
    act(() => within(card).getByRole('button', { name: 'Accept' }).click());
    await settle();
    expect(calls).toEqual([{ t: 'pvpAnswer', d: { offer: 'of-1', accept: true } }]);
    expect(card).toHaveTextContent('Waiting for the others…');
    expect(within(card).queryByRole('button')).not.toBeInTheDocument();
  });

  it('Decline sends the answer; a refusal shows; the card goes once the offer lapses', async () => {
    const { socket, calls } = fakeSocket([refusal(ERR.PVP_OFFER_GONE)]);
    render(<MatchOffer socket={socket} />);
    offer('of-1', 0);
    expect(screen.getByRole('alertdialog')).toHaveTextContent('1v1 · no bots');
    act(() => screen.getByRole('button', { name: 'Decline' }).click());
    await settle();
    expect(calls).toEqual([{ t: 'pvpAnswer', d: { offer: 'of-1', accept: false } }]);
    expect(screen.getByRole('alert')).toHaveTextContent('That match is gone.');
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('a dead player can’t accept until back on their feet, and a new offer starts afresh', () => {
    const { socket } = fakeSocket();
    render(<MatchOffer socket={socket} />);
    act(() => character.getState().setDead(true));
    offer();
    expect(screen.getByRole('button', { name: 'Accept' })).toBeDisabled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Get back on your feet to accept.');
    act(() => character.getState().setDead(false));
    expect(screen.getByRole('button', { name: 'Accept' })).toBeEnabled();
    act(() => pvp.getState().accepted('of-1'));
    offer('of-2', 2);
    expect(screen.getByRole('alertdialog')).toHaveTextContent('1v1 · 2 bots');
    expect(screen.getByRole('button', { name: 'Accept' })).toBeEnabled();
  });
});

describe('MatchResult', () => {
  beforeEach(() => {
    resetStores();
    pvp.getState().applyMatchState(
      create(MatchStateSchema, {
        match: 'mt-1',
        mode: 'duel',
        phase: MatchPhase.RESULT,
        wins: [1, 2],
        sides: [
          { fighters: [{ id: 'b-1', name: 'Ash', bot: true }] },
          { fighters: [{ id: 'c-ayla', name: 'Ayla' }] },
        ],
      }),
    );
  });

  const ended = (result: MatchOutcome, rating?: object) =>
    act(() =>
      pvp
        .getState()
        .applyMatchEnded(
          create(MatchEndedSchema, { result, wins: [1, 2], ...(rating && { rating }) }),
          'c-ayla',
        ),
    );

  it('shows the outcome, the rounds from the player’s side, the rating’s change and a new rank; Return closes it', () => {
    render(<MatchResult />);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    ended(MatchOutcome.WIN, {
      before: 1180,
      after: 1212,
      rankBefore: 'bronze',
      rankAfter: 'silver',
    });
    const card = screen.getByRole('dialog', { name: 'Match result' });
    expect(within(card).getByText('Victory')).toHaveClass('result-win');
    expect(card).toHaveTextContent('1v1');
    expect(card).toHaveTextContent('Rounds 2 – 1');
    expect(card).toHaveTextContent('Rating 1180 → 1212 (+32)');
    expect(card).toHaveTextContent('New rank: Silver');
    act(() => within(card).getByRole('button', { name: 'Return' }).click());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('a loss shows the rating falling, placements left in place of a rank, and no rating where the match had none', () => {
    render(<MatchResult />);
    ended(MatchOutcome.LOSS, {
      before: 1000,
      after: 985,
      rankBefore: 'bronze',
      rankAfter: 'silver',
      placementLeft: 1,
    });
    const card = screen.getByRole('dialog');
    expect(card).toHaveTextContent('Defeat');
    expect(card).toHaveTextContent('Rating 1000 → 985 (−15)');
    expect(card).toHaveTextContent('1 placement match left');
    expect(card).not.toHaveTextContent('New rank');
    ended(MatchOutcome.WIN, {
      before: 1212,
      after: 1230,
      rankBefore: 'silver',
      rankAfter: 'silver',
    });
    expect(card).toHaveTextContent('Rating 1212 → 1230 (+18)');
    expect(card).not.toHaveTextContent('New rank'); // the same rank
    ended(MatchOutcome.DRAW);
    expect(card).toHaveTextContent('Draw');
    expect(card).not.toHaveTextContent('Rating');
  });
});
