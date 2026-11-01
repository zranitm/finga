import { create } from '@bufbuild/protobuf';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  PartyInvitedSchema,
  PartyStateSchema,
  PartyViewSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { isMember, party } from './party';

const ayla = {
  characterId: 'c-ayla',
  name: 'Ayla',
  level: 2,
  hp: 250,
  maxHp: 250,
  mapId: 'forest',
  online: true,
};
const robin = {
  characterId: 'c-robin',
  name: 'Robin',
  level: 1,
  hp: 105,
  maxHp: 105,
  mapId: 'town',
  online: false,
};
const invited = (inviteId: string, fromName: string) =>
  create(PartyInvitedSchema, { inviteId, from: `c-${fromName}`, fromName });

describe('party store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('leader and member order follow the payload', () => {
    const view = create(PartyViewSchema, { leader: 'c-robin', members: [robin, ayla] });
    party.getState().applyPartyState(create(PartyStateSchema, { party: view }));
    expect(party.getState().party).toEqual(view);
    expect(party.getState().party?.members.map((m) => m.name)).toEqual(['Robin', 'Ayla']);
    expect(isMember(party.getState().party, 'c-ayla')).toBe(true);
    expect(isMember(party.getState().party, 'c-nobody')).toBe(false);
  });

  it('a PartyState with no party clears the members', () => {
    party
      .getState()
      .applyPartyState(
        create(PartyStateSchema, { party: { leader: 'c-ayla', members: [ayla, robin] } }),
      );
    party.getState().applyPartyState(create(PartyStateSchema));
    expect(party.getState().party).toBeNull();
    expect(isMember(null, 'c-ayla')).toBe(false);
  });

  it('invites queue by id, a repeat replaces, and a dismissal removes', () => {
    party.getState().applyPartyInvited(invited('inv-1', 'Ayla'), 1000);
    party.getState().applyPartyInvited(invited('inv-2', 'Robin'), 2000);
    party.getState().applyPartyInvited(invited('inv-1', 'Ayla'), 3000);
    expect(party.getState().invites).toEqual([
      { inviteId: 'inv-2', from: 'c-Robin', fromName: 'Robin', at: 2000 },
      { inviteId: 'inv-1', from: 'c-Ayla', fromName: 'Ayla', at: 3000 },
    ]);
    party.getState().dismissInvite('inv-2');
    expect(party.getState().invites.map((i) => i.inviteId)).toEqual(['inv-1']);
  });

  it('Welcome carries the party or nothing', () => {
    const party1 = { leader: 'c-ayla', members: [ayla] };
    party.getState().applyWelcome(create(WelcomeSchema, { mapId: 'forest', party: party1 }));
    expect(party.getState().party?.leader).toBe('c-ayla');
    party.getState().applyWelcome(create(WelcomeSchema, { mapId: 'forest' }));
    expect(party.getState().party).toBeNull();
    party.getState().applyPartyInvited(invited('inv-1', 'Robin'));
    party.getState().clear();
    expect(party.getState().invites).toEqual([]);
  });
});
