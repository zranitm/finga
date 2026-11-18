import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { MatchPhase, MatchStateSchema, PvpQueuedSchema } from '@/protocol/messages';
import { pvp } from '@/state/pvp';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';
import { user } from '@/testing/user';

import { ArenaWindow } from './ArenaWindow';
import { QueueTag } from './QueueTag';

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

const queued = (sinceMs = 0) =>
  act(() => pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'duel', sinceMs })));

describe('ArenaWindow', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows a card for each mode the queue takes, and Queue asks for its queue; the queue’s answer turns it to the wait and Leave queue', async () => {
    const { socket, calls } = fakeSocket();
    render(<ArenaWindow socket={socket} onClose={vi.fn()} />);
    const card = screen.getByRole('listitem', { name: '1v1' });
    expect(card).toHaveTextContent('1 against 1, best of 3 rounds.');
    expect(card).toHaveTextContent('Rated');
    expect(
      within(screen.getByRole('list', { name: 'Modes' })).getAllByRole('listitem'),
    ).toHaveLength(1);
    expect(screen.getByText(/level 30/)).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: 'Queue' }));
    expect(calls).toEqual([{ t: 'pvpQueue', d: { mode: 'duel' } }]);
    queued(5000);
    expect(within(card).getByLabelText('Waiting 0:05')).toBeInTheDocument();
    expect(screen.getByText(/Play on while you wait/)).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: 'Leave queue' }));
    expect(calls.at(-1)).toEqual({ t: 'pvpLeaveQueue', d: {} });
  });

  it('shows the server’s refusal, and holds Queue during a match', async () => {
    const { socket } = fakeSocket([refusal(ERR.PVP_NOT_PROMOTED)]);
    render(<ArenaWindow socket={socket} onClose={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Queue' }));
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Choose a class before you fight in the Arena.',
    );
    act(() =>
      pvp
        .getState()
        .applyMatchState(
          create(MatchStateSchema, { match: 'mt-1', mode: 'duel', phase: MatchPhase.PREP }),
        ),
    );
    expect(screen.getByRole('button', { name: 'Queue' })).toBeDisabled();
    expect(screen.getByText('You’re in a match.')).toBeInTheDocument();
  });
});

describe('QueueTag', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the mode and the time waited, counting on, over the minimap or in its corner with it off; ✕ leaves the queue', () => {
    const { socket, calls } = fakeSocket();
    render(<QueueTag socket={socket} />);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    queued(61_000);
    const tag = screen.getByRole('status', { name: 'In the 1v1 queue' });
    expect(tag).toHaveTextContent('1v1 queue1:01');
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(tag).toHaveTextContent('1:11');
    expect(tag).not.toHaveClass('low');
    act(() => settings.getState().setShowMinimap(false));
    expect(tag).toHaveClass('low');
    act(() => within(tag).getByRole('button', { name: 'Leave the queue' }).click());
    expect(calls).toEqual([{ t: 'pvpLeaveQueue', d: {} }]);
  });

  it('keeps no clock running till the player queues, and lets go of it when they leave', () => {
    render(<QueueTag socket={fakeSocket().socket} />);
    expect(vi.getTimerCount()).toBe(0);
    queued();
    expect(vi.getTimerCount()).toBe(1);
    act(() => pvp.getState().applyUnqueued());
    expect(vi.getTimerCount()).toBe(0);
  });

  it('says why a leave was refused', async () => {
    const { socket } = fakeSocket([refusal(ERR.PVP_NOT_QUEUED)]);
    render(<QueueTag socket={socket} />);
    queued();
    act(() => screen.getByRole('button', { name: 'Leave the queue' }).click());
    await act(() => Promise.resolve());
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['You’re not in a queue.']);
  });
});
