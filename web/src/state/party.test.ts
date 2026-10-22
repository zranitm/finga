import { beforeEach, describe, expect, it } from 'vitest';

import type { PartyView } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { isMember, party } from './party';

const ayla = {
  character_id: 'c-ayla',
  name: 'Ayla',
  level: 2,
  hp: 250,
  max_hp: 250,
  map_id: 'forest',
  online: true,
};
const robin = {
  character_id: 'c-robin',
  name: 'Robin',
  level: 1,
  hp: 105,
  max_hp: 105,
  map_id: 'town',
  online: false,
};

describe('party store', () => {
  beforeEach(() => {
    resetStores();
  });

  it('leader and member order follow the payload', () => {
    const view: PartyView = { leader: 'c-robin', members: [robin, ayla] };
    party.getState().applyPartyState({ party: view });
    expect(party.getState().party).toEqual(view);
    expect(party.getState().party?.members.map((m) => m.name)).toEqual(['Robin', 'Ayla']);
    expect(isMember(party.getState().party, 'c-ayla')).toBe(true);
    expect(isMember(party.getState().party, 'c-nobody')).toBe(false);
  });

  it('a PartyState with party null clears the members', () => {
    party.getState().applyPartyState({ party: { leader: 'c-ayla', members: [ayla, robin] } });
    party.getState().applyPartyState({ party: null });
    expect(party.getState().party).toBeNull();
    expect(isMember(null, 'c-ayla')).toBe(false);
  });

  it('invites queue by id, a repeat replaces, and a dismissal removes', () => {
    const inv = { invite_id: 'inv-1', from: 'c-ayla', from_name: 'Ayla' };
    party.getState().applyPartyInvited(inv);
    party.getState().applyPartyInvited({ invite_id: 'inv-2', from: 'c-robin', from_name: 'Robin' });
    party.getState().applyPartyInvited(inv);
    expect(party.getState().invites.map((i) => i.invite_id)).toEqual(['inv-2', 'inv-1']);
    party.getState().dismissInvite('inv-2');
    expect(party.getState().invites.map((i) => i.invite_id)).toEqual(['inv-1']);
  });

  it('Welcome carries the party or nothing', () => {
    const base = {
      token: 't',
      character: ayla as never,
      inventory: [],
      map_id: 'forest',
      tick: 1,
      server_time: 0,
    };
    party.getState().applyWelcome({ ...base, party: { leader: 'c-ayla', members: [ayla] } });
    expect(party.getState().party?.leader).toBe('c-ayla');
    party.getState().applyWelcome(base);
    expect(party.getState().party).toBeNull();
    party.getState().applyPartyInvited({ invite_id: 'inv-1', from: 'c-robin', from_name: 'Robin' });
    party.getState().clear();
    expect(party.getState().invites).toEqual([]);
  });
});
