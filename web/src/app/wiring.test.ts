import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, getToken, setToken } from '@/net/token';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { toasts } from '@/state/toasts';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { applyStatus, wireSocket } from './wiring';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  max_hp: 204,
  mp: 28,
  max_mp: 28,
};
const potion = { item_id: 'i1', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };
const welcome = {
  token: 'tok-rotated',
  character: ayla,
  inventory: [potion],
  party: {
    leader: 'c-ayla',
    members: [
      {
        character_id: 'c-ayla',
        name: 'Ayla',
        level: 1,
        hp: 204,
        max_hp: 204,
        map_id: 'town',
        online: true,
      },
    ],
  },
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

async function connectedSocket(): Promise<{ socket: GameSocket; ws: FakeWebSocket }> {
  setToken('tok-login');
  const socket = new GameSocket({
    url: 'ws://game.test/ws',
    WebSocket: fakeWebSocketClass,
    backoff: { baseMs: 10, capMs: 10 },
  });
  wireSocket(socket);
  const p = socket.connect('c-ayla');
  const ws = FakeWebSocket.latest();
  ws.open();
  ws.push({ t: 'Welcome', d: welcome });
  await p;
  return { socket, ws };
}

describe('wiring', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
    resetStores();
    session.getState().logIn({ id: 'alice', username: 'alice' });
  });
  afterEach(() => {
    vi.useRealTimers();
    clearToken();
  });

  it('Welcome lands in the character, inventory and party stores, and the connection is connected', async () => {
    await connectedSocket();
    expect(character.getState().character?.name).toBe('Ayla');
    expect(character.getState().expToNext).toBe(20);
    expect(inventory.getState().slots.use[0]).toEqual(potion);
    expect(party.getState().party?.leader).toBe('c-ayla');
    expect(connection.getState().status).toBe('connected');
    expect(getToken()).toBe('tok-rotated');
  });

  it.each([
    {
      name: 'InventoryChanged',
      frame: {
        t: 'InventoryChanged',
        d: {
          slots: [
            { tab: 'use', slot: 0 },
            { tab: 'use', slot: 3, item: { ...potion, slot: 3 } },
          ],
        },
      },
      check: () => expect(inventory.getState().slots.use[3]?.slot).toBe(3),
    },
    {
      name: 'PartyState',
      frame: { t: 'PartyState', d: { party: null } },
      check: () => expect(party.getState().party).toBeNull(),
    },
    {
      name: 'PartyInvited',
      frame: { t: 'PartyInvited', d: { invite_id: 'inv-1', from: 'c-robin', from_name: 'Robin' } },
      check: () => expect(party.getState().invites[0]?.from_name).toBe('Robin'),
    },
    {
      name: 'ChatMessage',
      frame: {
        t: 'ChatMessage',
        d: { channel: 'map', sender: 'c-robin', sender_name: 'Robin', text: 'hi' },
      },
      check: () => expect(chat.getState().lines[0]?.text).toBe('hi'),
    },
    {
      name: 'LevelUp',
      frame: { t: 'LevelUp', d: { character: { ...ayla, level: 2, exp: 0 } } },
      check: () =>
        expect(character.getState()).toMatchObject({ character: { level: 2 }, expToNext: 65 }),
    },
    {
      name: 'ExpGained',
      frame: { t: 'ExpGained', d: { amount: 5, exp: 15 } },
      check: () => expect(character.getState().character?.exp).toBe(15),
    },
    {
      name: 'Pong',
      frame: { t: 'Pong', d: { client_time: Date.now() - 30, server_time: 1 } },
      check: () => expect(connection.getState().rtt).toBeGreaterThanOrEqual(30),
    },
  ])('a pushed $name lands in its store', async ({ frame, check }) => {
    const { ws } = await connectedSocket();
    ws.push(frame);
    check();
  });

  it('a Kicked frame sets the connection to kicked and returns to the login screen', async () => {
    const { ws } = await connectedSocket();
    ws.push({ t: 'Kicked', d: { reason: 'logged_in_elsewhere' } });
    ws.closeFromServer(4003);
    expect(connection.getState()).toMatchObject({ status: 'kicked', kick: 'logged_in_elsewhere' });
    expect(session.getState().loggedIn).toBe(false);
    expect(character.getState().character).toBeNull();
    expect(inventory.getState().slots.use[0]).toBeNull();
    expect(party.getState().party).toBeNull();
    expect(chat.getState().lines).toEqual([]);
  });

  it('a reconnect shows as reconnecting with its attempt, and a user close as disconnected', async () => {
    const { socket, ws } = await connectedSocket();
    ws.closeFromServer(4002);
    expect(connection.getState()).toMatchObject({ status: 'reconnecting', attempt: 1 });
    expect(session.getState().loggedIn).toBe(true);
    await vi.advanceTimersByTimeAsync(20);
    socket.close();
    expect(connection.getState().status).toBe('disconnected');
    expect(session.getState().loggedIn).toBe(true);
  });

  it('a session that ended logs out; the other statuses map one to one', () => {
    applyStatus({ status: 'connecting' });
    expect(connection.getState().status).toBe('connecting');
    applyStatus({ status: 'idle' });
    expect(connection.getState().status).toBe('disconnected');
    applyStatus({ status: 'closed', reason: 'no_token' });
    expect(connection.getState().status).toBe('disconnected');
    applyStatus({ status: 'closed', reason: 'session_ended', kick: 'session_expired', code: 4003 });
    expect(connection.getState()).toMatchObject({ status: 'kicked', kick: 'session_expired' });
    expect(session.getState().loggedIn).toBe(false);
  });

  it.each([
    {
      name: 'inventory_full',
      d: { ok: false, code: 'inventory_full' },
      text: 'Your inventory is full.',
    },
    {
      name: 'unavailable',
      d: { ok: false, code: 'unavailable' },
      text: 'The server is busy. Try again.',
    },
    {
      name: 'another refusal with a message',
      d: { ok: false, code: 'invalid_argument', message: 'nothing to pick up' },
      text: 'nothing to pick up',
    },
  ])('a Response with no request ID and $name shows a toast', async ({ d, text }) => {
    const { ws } = await connectedSocket();
    ws.push({ t: 'Response', d });
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual([text]);
  });

  it('a Response with no request ID that is ok shows nothing', async () => {
    const { ws } = await connectedSocket();
    ws.push({ t: 'Response', d: { ok: true } });
    expect(toasts.getState().toasts).toEqual([]);
  });

  it('unwiring stops the flow', async () => {
    setToken('tok-login');
    const socket = new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass });
    const off = wireSocket(socket);
    off();
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push({ t: 'Welcome', d: welcome });
    await p;
    expect(character.getState().character).toBeNull();
    expect(connection.getState().status).toBe('disconnected');
  });
});
