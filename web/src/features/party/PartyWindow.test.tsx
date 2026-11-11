import { create } from '@bufbuild/protobuf';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ERR } from '@/protocol/errors';
import { PartyInvitedSchema, PartyStateSchema } from '@/protocol/messages';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { refusal } from '@/testing/refusal';
import { resetStores } from '@/testing/reset-stores';

import { INVITE_SECONDS, InviteToasts } from './InviteToast';
import { MAX_PARTY, PartyWindow } from './PartyWindow';

const member = (
  id: string,
  name: string,
  extra: Partial<{ level: number; hp: number; online: boolean; mapId: string }> = {},
) => ({
  characterId: id,
  name,
  level: 1,
  hp: 100,
  maxHp: 100,
  mapId: 'forest',
  online: true,
  ...extra,
});
type Member = ReturnType<typeof member>;

const partyOf = (leader: string, members: Member[]) =>
  create(PartyStateSchema, { party: { leader, members } });
const invited = (inviteId: string, from: string, fromName: string) =>
  create(PartyInvitedSchema, { inviteId, from, fromName });

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
    expect(calls).toEqual([{ t: 'partyInvite', d: { characterId: 'c-robin' } }]);
  });

  it('a PartyState with four members disables invite, and one with no party closes the panel', () => {
    const { socket } = fakeSocket();
    render(<PartyWindow socket={socket} me="c-ayla" onClose={vi.fn()} />);
    act(() =>
      party
        .getState()
        .applyPartyState(
          partyOf('c-ayla', [
            member('c-ayla', 'Ayla'),
            member('c-a', 'A'),
            member('c-b', 'B'),
            member('c-c', 'C'),
          ]),
        ),
    );
    expect(
      screen
        .getAllByRole('button', { name: 'Invite' })
        .every((b) => (b as HTMLButtonElement).disabled),
    ).toBe(true);
    expect(screen.getByText(`The party is full (${MAX_PARTY}).`)).toBeInTheDocument();
    expect(screen.getByLabelText('Members').children).toHaveLength(4);
    act(() => party.getState().applyPartyState(create(PartyStateSchema)));
    expect(screen.queryByLabelText('Members')).not.toBeInTheDocument();
    expect(screen.getByText('You are not in a party.')).toBeInTheDocument();
  });

  it('the leader sees kick and promote for the others, members see leave only, and a refusal shows its words', async () => {
    const { socket, calls } = fakeSocket([refusal(ERR.NOT_PARTY_MEMBER), { ok: true }]);
    render(<PartyWindow socket={socket} me="c-ayla" onClose={vi.fn()} />);
    act(() =>
      party
        .getState()
        .applyPartyState(
          partyOf('c-ayla', [
            member('c-ayla', 'Ayla'),
            member('c-robin', 'Robin', { online: false, hp: 40 }),
          ]),
        ),
    );
    expect(screen.getByText('Ayla (leader)')).toBeInTheDocument();
    expect(screen.getByRole('meter', { name: 'Robin HP' })).toHaveAttribute('aria-valuenow', '40');
    expect(screen.getByText(/offline/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Kick' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'That character isn’t in your party.',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Promote' }));
    expect(calls.map((c) => c.t)).toEqual(['partyKick', 'partyPromote']);
    expect(calls[1]?.d).toEqual({ characterId: 'c-robin' });
    // Robin is not in the nearby invite list any more: already a member.
    expect(screen.getAllByRole('button', { name: 'Invite' })).toHaveLength(1);
    act(() =>
      party
        .getState()
        .applyPartyState(
          partyOf('c-robin', [member('c-robin', 'Robin'), member('c-ayla', 'Ayla')]),
        ),
    );
    expect(screen.queryByRole('button', { name: 'Kick' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Leave party' }));
    expect(calls[2]).toEqual({ t: 'partyLeave', d: {} });
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
    act(() => party.getState().applyPartyInvited(invited('inv-1', 'c-robin', 'Robin'), now));
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
    expect(calls).toEqual([{ t: 'partyRespond', d: { inviteId: 'inv-1', accept: true } }]);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();

    act(() => party.getState().applyPartyInvited(invited('inv-2', 'c-bruno', 'Bruno'), now));
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
    act(() => party.getState().applyPartyInvited(invited('inv-3', 'c-robin', 'Robin')));
    act(() => {
      screen.getByRole('button', { name: 'Decline' }).click();
    });
    expect(calls).toEqual([{ t: 'partyRespond', d: { inviteId: 'inv-3', accept: false } }]);
  });
});
