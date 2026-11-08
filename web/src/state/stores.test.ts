import { beforeEach, describe, expect, it } from 'vitest';

import { KickReason } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { connection } from './connection';
import { session } from './session';
import { ui } from './ui';

describe('session, connection and ui stores', () => {
  beforeEach(() => {
    resetStores();
  });

  it('session logs in and out', () => {
    expect(session.getState().loggedIn).toBe(false);
    session.getState().logIn({ id: 'a1', username: 'alice' });
    expect(session.getState()).toMatchObject({ loggedIn: true, account: { username: 'alice' } });
    session.getState().logOut();
    expect(session.getState()).toMatchObject({ loggedIn: false, account: null });
  });

  it('connection keeps the attempt while reconnecting and the reason when kicked', () => {
    connection.getState().setConnection('connecting');
    connection.getState().setConnection('reconnecting', { attempt: 3 });
    expect(connection.getState()).toMatchObject({ status: 'reconnecting', attempt: 3, kick: null });
    connection.getState().setConnection('kicked', { kick: KickReason.LOGGED_IN_ELSEWHERE });
    expect(connection.getState()).toMatchObject({
      status: 'kicked',
      attempt: 0,
      kick: KickReason.LOGGED_IN_ELSEWHERE,
    });
    connection.getState().setConnection('connected', { kick: KickReason.ABUSE });
    expect(connection.getState().kick).toBeNull(); // a kick reason only makes sense when kicked
    connection.getState().setRtt(42);
    expect(connection.getState().rtt).toBe(42);
  });

  it('ui toggles, opens and closes windows', () => {
    ui.getState().toggleWindow('inventory');
    expect(ui.getState().windows.inventory).toBe(true);
    ui.getState().toggleWindow('inventory');
    expect(ui.getState().windows.inventory).toBe(false);
    ui.getState().openWindow('party');
    ui.getState().openWindow('party');
    expect(ui.getState().windows).toEqual({
      inventory: false,
      character: false,
      party: true,
      settings: false,
    });
    ui.getState().closeWindow('party');
    expect(ui.getState().windows.party).toBe(false);
  });

  it('a subscriber hears a change and reset brings the initial state back', () => {
    const seen: boolean[] = [];
    const off = session.subscribe((s) => seen.push(s.loggedIn));
    session.getState().logIn({ id: 'a1', username: 'alice' });
    off();
    session.getState().logOut();
    expect(seen).toEqual([true]);
    session.getState().logIn({ id: 'a1', username: 'alice' });
    session.reset();
    expect(session.getState().loggedIn).toBe(false);
  });
});
