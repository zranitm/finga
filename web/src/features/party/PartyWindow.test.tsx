import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RequestError } from '@/net/socket';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { resetStores } from '@/testing/reset-stores';

import { INVITE_SECONDS, InviteToasts } from './InviteToast';
import { MAX_PARTY, PartyWindow } from './PartyWindow';

const member = (
  id: string,
  name: string,
  extra: Partial<{ level: number; hp: number; online: boolean; map_id: string }> = {},
) => ({
  character_id: id,
  name,
  level: 1,
  hp: 100,
  max_hp: 100,
  map_id: 'forest',
  online: true,
  ...extra,
});

function fakeSocket(answers: readonly (object | Error)[] = []) {
  const calls: { t: string; d: unknown }[] = [];
  const request = vi.fn((t: string, d: unknown) => {
    calls.push({ t, d });
    const a = answers[calls.length - 1] ?? { ok: true };
    return a instanceof Error ? Promise.reject(a) : Promise.resolve(a);
  });
  return { socket: { request } as never, calls };
}

describe('PartyWindow', () => {
  beforeEach(() => {
    resetStores();
    nearby.getState().setNearby([
      { id: 'c-robin', name: 'Robin' },
      { id: 'c-bruno', name: 'Bruno' },
    ]);
  });

  it('inviting from the nearby list sends PartyInvite with that character ID (and the socket adds the request ID)', async () => {
    const { socket, calls } = fakeSocket();
    render(<PartyWindow socket={socket} me="c-ayla" onClose={vi.fn()} />);
    expect(screen.getByText('You are not in a party.')).toBeInTheDocument();
    const buttons = screen.getAllByRole('button', { name: 'Invite' });
    expect(buttons).toHaveLength(2);
    await userEvent.click(buttons[1]!); // sorted by name: Bruno, Robin
    expect(calls).toEqual([{ t: 'PartyInvite', d: { character_id: 'c-robin' } }]);
  });

  it('a PartyState with four members disables invite, and party: null closes the panel', () => {
    const { socket } = fakeSocket();
    render(<PartyWindow socket={socket} me="c-ayla" onClose={vi.fn()} />);
    act(() =>
      party.getState().applyPartyState({
        party: {
          leader: 'c-ayla',
          members: [
            member('c-ayla', 'Ayla'),
            member('c-a', 'A'),
            member('c-b', 'B'),
            member('c-c', 'C'),
          ],
        },
      }),
    );
    expect(
      screen
        .getAllByRole('button', { name: 'Invite' })
        .every((b) => (b as HTMLButtonElement).disabled),
    ).toBe(true);
    expect(screen.getByText(`The party is full (${MAX_PARTY}).`)).toBeInTheDocument();
    expect(screen.getByLabelText('Members').children).toHaveLength(4);
    act(() => party.getState().applyPartyState({ party: null }));
    expect(screen.queryByLabelText('Members')).not.toBeInTheDocument();
    expect(screen.getByText('You are not in a party.')).toBeInTheDocument();
  });

  it('the leader sees kick and promote for the others, members see leave only, and a refusal shows the reason', async () => {
    const { socket, calls } = fakeSocket([
      new RequestError('not_party_member', 'no such member'),
      { ok: true },
    ]);
    render(<PartyWindow socket={socket} me="c-ayla" onClose={vi.fn()} />);
    act(() =>
      party.getState().applyPartyState({
        party: {
          leader: 'c-ayla',
          members: [
            member('c-ayla', 'Ayla'),
            member('c-robin', 'Robin', { online: false, hp: 40 }),
          ],
        },
      }),
    );
    expect(screen.getByText('Ayla (leader)')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'Robin HP' })).toHaveAttribute('aria-valuenow', '40');
    expect(screen.getByText(/offline/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Kick' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('no such member');
    await userEvent.click(screen.getByRole('button', { name: 'Promote' }));
    expect(calls.map((c) => c.t)).toEqual(['PartyKick', 'PartyPromote']);
    expect(calls[1]?.d).toEqual({ character_id: 'c-robin' });
    // Robin is not in the nearby invite list any more: already a member.
    expect(screen.getAllByRole('button', { name: 'Invite' })).toHaveLength(1);
    act(() =>
      party.getState().applyPartyState({
        party: {
          leader: 'c-robin',
          members: [member('c-robin', 'Robin'), member('c-ayla', 'Ayla')],
        },
      }),
    );
    expect(screen.queryByRole('button', { name: 'Kick' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Leave party' }));
    expect(calls[2]).toEqual({ t: 'PartyLeave', d: {} });
  });
});

describe('InviteToasts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('a PartyInvited toast counts down from 30 and disappears; accept sends PartyRespond', () => {
    const { socket, calls } = fakeSocket();
    let now = 100_000;
    render(<InviteToasts socket={socket} now={() => now} />);
    act(() =>
      party
        .getState()
        .applyPartyInvited({ invite_id: 'inv-1', from: 'c-robin', from_name: 'Robin' }, now),
    );
    expect(screen.getByRole('status')).toHaveTextContent(
      `Robin invites you to a party (${INVITE_SECONDS}s)`,
    );
    act(() => {
      now += 5000;
      vi.advanceTimersByTime(5000);
    });
    expect(screen.getByRole('status')).toHaveTextContent('(25s)');
    act(() => {
      screen.getByRole('button', { name: 'Accept' }).click();
    });
    expect(calls).toEqual([{ t: 'PartyRespond', d: { invite_id: 'inv-1', accept: true } }]);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    act(() =>
      party
        .getState()
        .applyPartyInvited({ invite_id: 'inv-2', from: 'c-bruno', from_name: 'Bruno' }, now),
    );
    act(() => {
      now += INVITE_SECONDS * 1000;
      vi.advanceTimersByTime(INVITE_SECONDS * 1000);
    });
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(party.getState().invites).toEqual([]);
  });

  it('decline sends PartyRespond with accept false', () => {
    const { socket, calls } = fakeSocket();
    render(<InviteToasts socket={socket} />);
    act(() =>
      party
        .getState()
        .applyPartyInvited({ invite_id: 'inv-3', from: 'c-robin', from_name: 'Robin' }),
    );
    act(() => {
      screen.getByRole('button', { name: 'Decline' }).click();
    });
    expect(calls).toEqual([{ t: 'PartyRespond', d: { invite_id: 'inv-3', accept: false } }]);
  });
});
