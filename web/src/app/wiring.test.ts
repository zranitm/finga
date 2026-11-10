import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, getToken, setToken } from '@/net/token';
import { ChatChannel, KickReason } from '@/protocol/messages';
import { character } from '@/state/character';
import { chat } from '@/state/chat';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { party } from '@/state/party';
import { session } from '@/state/session';
import { toasts } from '@/state/toasts';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { applyStatus, wireSocket } from './wiring';

const ayla = {
  id: 'c-ayla',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  maxHp: 204,
  mp: 28,
  maxMp: 28,
};
const potion = { itemId: 'i1', template: 'red_potion', tab: 'use', slot: 0, quantity: 5 };
const welcome: ServerInit<'welcome'> = {
  token: 'tok-rotated',
  character: ayla,
  inventory: [potion],
  mapId: 'town',
  tick: 1,
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
  ws.push('welcome', welcome);
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

  it('Welcome lands in the character and inventory stores, leaves the party to the PartyState after it, and connects', async () => {
    await connectedSocket();
    expect(character.getState().character?.name).toBe('Ayla');
    expect(character.getState().expToNext).toBe(20);
    expect(inventory.getState().slots.use[0]).toMatchObject(potion);
    expect(party.getState().party).toBeNull();
    expect(connection.getState().status).toBe('connected');
    expect(getToken()).toBe('tok-rotated');
  });

  it.each([
    {
      name: 'InventoryChanged',
      push: (ws: FakeWebSocket) =>
        ws.push('inventoryChanged', {
          slots: [
            { tab: 'use', slot: 0 },
            { tab: 'use', slot: 3, item: { ...potion, slot: 3 } },
          ],
        }),
      check: () => expect(inventory.getState().slots.use[3]?.slot).toBe(3),
    },
    {
      name: 'PartyState',
      push: (ws: FakeWebSocket) => ws.push('partyState', {}),
      check: () => expect(party.getState().party).toBeNull(),
    },
    {
      name: 'PartyInvited',
      push: (ws: FakeWebSocket) =>
        ws.push('partyInvited', { inviteId: 'inv-1', from: 'c-robin', fromName: 'Robin' }),
      check: () => expect(party.getState().invites[0]?.fromName).toBe('Robin'),
    },
    {
      name: 'ChatMessage',
      push: (ws: FakeWebSocket) =>
        ws.push('chatMessage', {
          channel: ChatChannel.MAP,
          sender: 'c-robin',
          senderName: 'Robin',
          text: 'hi',
        }),
      check: () => expect(chat.getState().lines[0]?.text).toBe('hi'),
    },
    {
      name: 'LevelUp',
      push: (ws: FakeWebSocket) =>
        ws.push('levelUp', { character: { ...ayla, level: 2, exp: 0n } }),
      check: () =>
        expect(character.getState()).toMatchObject({ character: { level: 2 }, expToNext: 65 }),
    },
    {
      name: 'StatsChanged',
      push: (ws: FakeWebSocket) =>
        ws.push('statsChanged', {
          sheet: { derivedWorn: { defense: 4 } },
          hp: 204,
          maxHp: 234,
          mp: 28,
          maxMp: 28,
        }),
      check: () =>
        expect(character.getState().character).toMatchObject({
          maxHp: 234,
          sheet: { derivedWorn: { defense: 4 } },
        }),
    },
    {
      name: 'InventoryChanged with equipment',
      push: (ws: FakeWebSocket) =>
        ws.push('inventoryChanged', {
          equipment: [
            { equipSlot: 'hat', item: { itemId: 'i-cap', template: 'leather_cap', quantity: 1 } },
          ],
          revision: 2n,
        }),
      check: () => expect(inventory.getState().equipment.hat?.itemId).toBe('i-cap'),
    },
    {
      name: 'ExpGained',
      push: (ws: FakeWebSocket) => ws.push('expGained', { amount: 5n, exp: 15n }),
      check: () => expect(character.getState().character?.exp).toBe(15n),
    },
    {
      name: 'Pong',
      push: (ws: FakeWebSocket) =>
        ws.push('pong', { clientTime: BigInt(Date.now() - 30), serverTime: 1n }),
      check: () => expect(connection.getState().rtt).toBeGreaterThanOrEqual(30),
    },
  ])('a pushed $name lands in its store', async ({ push, check }) => {
    const { ws } = await connectedSocket();
    push(ws);
    check();
  });

  it('a Kicked frame sets the connection to kicked and returns to the login screen', async () => {
    const { ws } = await connectedSocket();
    ws.push('kicked', { reason: KickReason.LOGGED_IN_ELSEWHERE });
    ws.closeFromServer(4003);
    expect(connection.getState()).toMatchObject({
      status: 'kicked',
      kick: KickReason.LOGGED_IN_ELSEWHERE,
    });
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
    applyStatus({
      status: 'closed',
      reason: 'session_ended',
      kick: KickReason.SESSION_EXPIRED,
      code: 4003,
    });
    expect(connection.getState()).toMatchObject({
      status: 'kicked',
      kick: KickReason.SESSION_EXPIRED,
    });
    expect(session.getState().loggedIn).toBe(false);
  });

  it.each([
    {
      name: 'unavailable (a service down or busy)',
      d: { ok: false, code: 'unavailable', message: 'the server is busy; try again' },
      text: 'The server is busy. Try again.',
    },
    {
      name: 'chat_rate_limited (a chat line too soon)',
      d: { ok: false, code: 'chat_rate_limited', message: 'slow down' },
      text: 'Too fast. Wait a moment.',
    },
    {
      name: 'rate_limited (any message too soon)',
      d: { ok: false, code: 'rate_limited', message: 'slow down' },
      text: 'Too fast. Wait a moment.',
    },
    {
      name: 'chat_no_party (a party line with no party)',
      d: { ok: false, code: 'chat_no_party', message: 'you have no party' },
      text: 'you have no party',
    },
    {
      name: 'another refusal with a message',
      d: { ok: false, code: 'target_not_on_map', message: 'that character is not on your map' },
      text: 'that character is not on your map',
    },
  ])('a Response with no request ID and $name shows a toast', async ({ d, text }) => {
    const { ws } = await connectedSocket();
    ws.push('response', d);
    expect(toasts.getState().toasts.map((t) => t.text)).toEqual([text]);
  });

  it('a Response with no request ID that is ok shows nothing', async () => {
    const { ws } = await connectedSocket();
    ws.push('response', { ok: true });
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
    ws.push('welcome', welcome);
    await p;
    expect(character.getState().character).toBeNull();
    expect(connection.getState().status).toBe('disconnected');
  });
});
