import { create } from '@bufbuild/protobuf';
import { act, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import {
  MatchPhase,
  MatchStateSchema,
  PartyStateSchema,
  PartyViewSchema,
  PvpQueuedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { pvpProfile } from '@/state/pvp-profile';
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

const noBoard = () => Promise.resolve({ mode: 'duel', rows: [] });

const queued = (sinceMs = 0, waitingRoom = false) =>
  act(() =>
    pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'duel', sinceMs, waitingRoom })),
  );

describe('ArenaWindow', () => {
  beforeEach(() => {
    resetStores();
  });

  it('shows a card for each mode the queue takes, and Queue asks for its queue; the queue’s answer turns it to the wait and Leave queue', async () => {
    const { socket, calls } = fakeSocket();
    render(<ArenaWindow socket={socket} board={noBoard} onClose={vi.fn()} />);
    const card = screen.getByRole('listitem', { name: '1v1' });
    expect(card).toHaveTextContent('1 against 1, best of 3 rounds.');
    expect(card).toHaveTextContent('Rated');
    expect(
      within(screen.getByRole('list', { name: 'Modes' })).getAllByRole('listitem'),
    ).toHaveLength(2);
    expect(screen.queryByRole('listitem', { name: '15v15' })).not.toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: '3v3' })).toHaveTextContent(
      '3 against 3, best of 3 rounds.',
    );
    expect(screen.getByText(/level 30/)).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: 'Queue' }));
    expect(calls).toEqual([{ t: 'pvpQueue', d: { mode: 'duel' } }]);
    queued(5000);
    expect(within(card).getByLabelText('Waiting 0:05')).toBeInTheDocument();
    queued(20_000, true);
    expect(within(card).getByLabelText(/^Waiting for room 0:/)).toBeInTheDocument();
    queued(21_000);
    expect(within(card).getByLabelText(/^Waiting 0:/)).toBeInTheDocument();
    expect(screen.getByText(/Play on while you wait/)).toBeInTheDocument();
    await user.click(within(card).getByRole('button', { name: 'Leave queue' }));
    expect(calls.at(-1)).toEqual({ t: 'pvpLeaveQueue', d: {} });
  });

  it('shows the server’s refusal, and holds Queue during a match', async () => {
    const { socket } = fakeSocket([refusal(ERR.PVP_NOT_PROMOTED)]);
    render(<ArenaWindow socket={socket} board={noBoard} onClose={vi.fn()} />);
    const card = screen.getByRole('listitem', { name: '1v1' });
    await user.click(within(card).getByRole('button', { name: 'Queue' }));
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Choose a class before you fight in the Arena.',
    );
    act(() =>
      pvp
        .getState()
        .applyMatchState(
          create(MatchStateSchema, { match: 'mt-1', mode: 'duel', phase: MatchPhase.PREP }),
          0,
        ),
    );
    for (const queue of screen.getAllByRole('button', { name: 'Queue' })) {
      expect(queue).toBeDisabled();
    }
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

  it('says while the match waits for room for its bots, and drops it when the queue says it has room', () => {
    render(<QueueTag socket={fakeSocket().socket} />);
    queued(20_000, true);
    const tag = screen.getByRole('status', { name: 'In the 1v1 queue' });
    expect(tag).toHaveTextContent('Waiting for room');
    expect(tag).toHaveTextContent('0:20');
    queued(21_000);
    expect(tag).not.toHaveTextContent('Waiting for room');
  });

  it('names the party members queued with the player, and none for one queued alone', () => {
    render(<QueueTag socket={fakeSocket().socket} />);
    act(() => {
      character
        .getState()
        .applyWelcome(
          create(WelcomeSchema, { character: { id: 'c-ayla', name: 'Ayla', class: 'warrior' } }),
        );
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: create(PartyViewSchema, {
            leader: 'c-ayla',
            members: [
              { characterId: 'c-ayla', name: 'Ayla' },
              { characterId: 'c-robin', name: 'Robin' },
              { characterId: 'c-jade', name: 'Jade' },
            ],
          }),
        }),
      );
      pvp
        .getState()
        .applyQueued(
          create(PvpQueuedSchema, { mode: 'team', members: ['c-ayla', 'c-robin', 'c-jade'] }),
        );
    });
    expect(screen.getByRole('status', { name: 'In the 3v3 queue' })).toHaveTextContent(
      '3v3 queuewith Robin and Jade0:00',
    );
    act(() =>
      pvp.getState().applyQueued(create(PvpQueuedSchema, { mode: 'team', members: ['c-ayla'] })),
    );
    expect(screen.getByRole('status')).not.toHaveTextContent('with');
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

describe('ArenaWindow standings and board', () => {
  beforeEach(() => {
    resetStores();
    act(() =>
      character
        .getState()
        .applyWelcome(
          create(WelcomeSchema, { character: { id: 'c-ayla', name: 'Ayla', class: 'warrior' } }),
        ),
    );
  });

  const standing = (mode: string, over: object = {}) => ({
    mode,
    shown: 1640,
    rank: 'gold',
    placement_left: 0,
    wins: 6,
    losses: 2,
    draws: 1,
    place: 12,
    ...over,
  });
  const row = (place: number, id: string, name: string, shown: number) => ({
    place,
    character_id: id,
    name,
    class: 'warrior',
    shown,
    rank: 'gold',
    wins: 9,
    losses: 1,
    draws: 0,
  });

  it('opening it reads the profile afresh, and each rated card shows the rating, rank and place once placed, or how far placement is', () => {
    act(() =>
      pvpProfile
        .getState()
        .apply([
          standing('duel'),
          standing('team', { placement_left: 3, wins: 1, losses: 1, draws: 0 }),
        ]),
    );
    render(<ArenaWindow socket={fakeSocket().socket} board={noBoard} onClose={vi.fn()} />);
    expect(pvpProfile.getState().asked).toBe(1);
    expect(
      within(screen.getByRole('listitem', { name: '1v1' })).getByLabelText('Your standing'),
    ).toHaveTextContent('1640 Gold · #12');
    expect(screen.getByRole('listitem', { name: '1v1' })).toHaveTextContent('6 W · 2 L · 1 D');
    expect(
      within(screen.getByRole('listitem', { name: '3v3' })).getByLabelText('Your standing'),
    ).toHaveTextContent('Placing (2 of 5)1 W · 1 L · 0 D');
  });

  it('a card shows no standing till the profile is read', () => {
    render(<ArenaWindow socket={fakeSocket().socket} board={noBoard} onClose={vi.fn()} />);
    expect(screen.queryByLabelText('Your standing')).toBeNull();
  });

  it('the Leaderboard lists a mode’s board, the player’s own row marked; another mode reads its own', async () => {
    const board = vi.fn((mode: string) =>
      Promise.resolve({
        mode,
        rows:
          mode === 'duel'
            ? [
                row(1, 'c-robin', 'Robin', 2100),
                row(2, 'c-ayla', 'Ayla', 1900),
                row(2, 'c-jade', 'Jade', 1900),
              ]
            : [row(1, 'c-jade', 'Jade', 1500)],
      }),
    );
    act(() =>
      pvpProfile
        .getState()
        .apply([
          standing('duel', { place: 2 }),
          standing('team', { place: 7, shown: 1200, rank: 'silver' }),
        ]),
    );
    render(<ArenaWindow socket={fakeSocket().socket} board={board} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'Leaderboard' }));
    const table = await screen.findByRole('table', { name: 'Leaderboard' });
    const rows = within(table).getAllByRole('row').slice(1);
    expect(rows.map((r) => r.textContent)).toEqual([
      '1RobinWarrior2100Gold9–1–0',
      '2AylaWarrior1900Gold9–1–0',
      '2JadeWarrior1900Gold9–1–0',
    ]);
    expect(rows[1]).toHaveAttribute('aria-current', 'true');
    expect(rows[0]).not.toHaveAttribute('aria-current');
    expect(screen.getByLabelText('Your place')).toHaveTextContent('You: #2, 1640 Gold'); // in sight however far down the list their row is
    await user.click(screen.getByRole('tab', { name: '3v3' }));
    expect(await screen.findByLabelText('Your place')).toHaveTextContent(
      'You: #7, 1200 Silver, 6 W · 2 L · 1 D.',
    );
    expect(board.mock.calls).toEqual([['duel'], ['team']]);
  });

  it('an answer for a mode the player left lands nowhere', async () => {
    let duel: (b: { mode: string; rows: ReturnType<typeof row>[] }) => void = () => undefined;
    const board = (mode: string) =>
      mode === 'duel'
        ? new Promise<{ mode: string; rows: ReturnType<typeof row>[] }>((r) => (duel = r))
        : Promise.resolve({ mode, rows: [row(1, 'c-jade', 'Jade', 1500)] });
    render(<ArenaWindow socket={fakeSocket().socket} board={board} onClose={vi.fn()} />);
    await user.click(screen.getByRole('tab', { name: 'Leaderboard' }));
    await user.click(screen.getByRole('tab', { name: '3v3' }));
    expect(await screen.findByRole('table', { name: 'Leaderboard' })).toHaveTextContent('Jade');
    await act(async () => {
      duel({ mode: 'duel', rows: [row(1, 'c-robin', 'Robin', 2100)] });
      await Promise.resolve(); // the answer's then runs
    });
    expect(screen.getByRole('table', { name: 'Leaderboard' })).toHaveTextContent('Jade');
  });

  it('a player still placing is told so under the board; an empty board and a failed read say so', async () => {
    const answers = [
      () => Promise.resolve({ mode: 'duel', rows: [] }),
      () => Promise.reject(refusal(ERR.RATE_LIMITED)),
    ];
    act(() => pvpProfile.getState().apply([standing('duel', { placement_left: 4 })]));
    render(
      <ArenaWindow
        socket={fakeSocket().socket}
        board={() => answers.shift()!()}
        onClose={vi.fn()}
      />,
    );
    await user.click(screen.getByRole('tab', { name: 'Leaderboard' }));
    expect(await screen.findByText('No one has placed yet.')).toBeInTheDocument();
    expect(screen.getByLabelText('Your place')).toHaveTextContent('You’re placing: 1 of 5 played.');
    await user.click(screen.getByRole('tab', { name: '3v3' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Too fast. Wait a moment.');
  });
});
