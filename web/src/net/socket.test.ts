import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { jsonCodec } from '@/protocol/codec';
import type { ProtocolError } from '@/protocol/codec';
import { ChatChannel, KickReason, WelcomeSchema } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { Input } from '@/sim/input';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';

import { GameSocket, PROTOCOL_VERSION, RequestError } from './socket';
import type { StatusEvent } from './socket';
import { clearToken, getToken, setToken } from './token';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

const welcome = create(WelcomeSchema, {
  token: 'tok-rotated-1',
  character: {
    id: 'c1',
    name: 'Ayla',
    class: 'warrior',
    level: 1,
    exp: 0n,
    stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
    hp: 204,
    maxHp: 204,
    mp: 28,
    maxMp: 28,
  },
  mapId: 'town',
  tick: 10,
  serverTime: 1_761_000_000_050n,
});

function newSocket(
  overrides: Partial<ConstructorParameters<typeof GameSocket>[0]> = {},
): GameSocket {
  return new GameSocket({
    url: 'ws://game.test/ws',
    WebSocket: fakeWebSocketClass,
    backoff: { baseMs: 100, capMs: 1000, random: () => 0.5 },
    now: () => Date.now(),
    ...overrides,
  });
}

async function connected(socket: GameSocket, token = 'tok-login'): Promise<FakeWebSocket> {
  setToken(token);
  const p = socket.connect('c1');
  const ws = FakeWebSocket.latest();
  ws.open();
  ws.push('welcome', { ...welcome, token: `${token}-rotated` });
  await p;
  return ws;
}

describe('GameSocket', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
  });
  afterEach(() => {
    vi.useRealTimers();
    clearToken();
  });

  it('sends Hello first with protocol version 1, and Welcome resolves connect and replaces the token', async () => {
    const socket = newSocket();
    const statuses: StatusEvent[] = [];
    socket.onStatus((e) => statuses.push(e));
    setToken('tok-login');
    const p = socket.connect('c1');
    const ws = FakeWebSocket.latest();
    expect(ws.url).toBe('ws://game.test/ws');
    expect(ws.sent).toHaveLength(0);
    ws.open();
    expect(ws.frames()).toMatchObject([
      {
        t: 'hello',
        d: { token: 'tok-login', protocolVersion: PROTOCOL_VERSION, characterId: 'c1' },
      },
    ]);
    ws.push('welcome', welcome);
    await expect(p).resolves.toEqual(welcome);
    expect(getToken()).toBe('tok-rotated-1');
    expect(statuses.map((s) => s.status)).toEqual(['connecting', 'connected']);
    expect(socket.currentStatus).toBe('connected');
  });

  it('asks for the binary subprotocol by default and reads binary frames as ArrayBuffers', async () => {
    const socket = newSocket();
    const ws = await connected(socket);
    expect(ws.protocols).toEqual(['finga.v1.proto']);
    expect(ws.binaryType).toBe('arraybuffer');
    expect(ws.sent[0]).toBeInstanceOf(Uint8Array);
  });

  it('with the JSON codec, asks for its subprotocol and speaks protojson text', async () => {
    const socket = newSocket({ codec: jsonCodec });
    const ws = await connected(socket);
    expect(ws.protocols).toEqual(['finga.v1.json']);
    expect(JSON.parse(String(ws.sent[0]))).toEqual({
      hello: { token: 'tok-login', protocol_version: 1, character_id: 'c1' },
    });
    expect(socket.currentStatus).toBe('connected');
  });

  it('refuses to connect without a token', async () => {
    const socket = newSocket();
    const statuses: StatusEvent[] = [];
    socket.onStatus((e) => statuses.push(e));
    await expect(socket.connect('c1')).rejects.toThrow(/no session token/);
    expect(FakeWebSocket.instances).toHaveLength(0);
    expect(statuses).toEqual([{ status: 'closed', reason: 'no_token' }]);
  });

  it('gives up on a connection that never answers Hello and reconnects', async () => {
    const socket = newSocket({ helloTimeoutMs: 1000 });
    setToken('tok-login');
    const p = socket.connect('c1');
    const first = FakeWebSocket.latest();
    first.open();
    const rejected = p.catch((e: Error) => e.message);
    await vi.advanceTimersByTimeAsync(1000);
    expect(first.closedWith?.reason).toBe('no welcome');
    expect(await rejected).toMatch(/before Welcome/);
    expect(socket.currentStatus).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(100); // backoff(0) with random 0.5 is 50 ms
    expect(FakeWebSocket.instances).toHaveLength(2);
  });

  describe('requests', () => {
    it('matches a Response by req and resolves with its data', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const p = socket.request('partyLeave', {});
      const frame = ws.frames().at(-1);
      expect(frame?.t).toBe('partyLeave');
      expect(frame?.req).toMatch(/^r-/);
      ws.push('response', { ok: true }, { req: frame?.req ?? '' });
      await expect(p).resolves.toMatchObject({ ok: true });
    });

    it('rejects with the server code when the Response is not ok', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const p = socket.request('inventoryMove', {
        idempotencyKey: 'k-1',
        itemId: 'i1',
        tab: 'use',
        slot: 3,
      });
      const frame = ws.frames().at(-1);
      ws.push(
        'response',
        { ok: false, code: 'inventory_full', message: 'full' },
        { req: frame?.req ?? '' },
      );
      const err = (await p.catch((e: unknown) => e)) as RequestError;
      expect(err).toBeInstanceOf(RequestError);
      expect(err.code).toBe('inventory_full');
      expect(err.message).toBe('full');
    });

    it('a refusal with a code this client does not know, and no message, is internal and refused', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const p = socket.request('partyLeave', {});
      ws.push('response', { ok: false, code: 'teapot' }, { req: ws.frames().at(-1)?.req ?? '' });
      const err = (await p.catch((e: unknown) => e)) as RequestError;
      expect([err.code, err.message]).toEqual(['internal', 'refused']);
    });

    it('times out when no Response comes', async () => {
      const socket = newSocket({ requestTimeoutMs: 2000 });
      await connected(socket);
      const p = socket.request('partyLeave', {});
      const result = p.catch((e: RequestError) => e.code);
      await vi.advanceTimersByTimeAsync(2000);
      expect(await result).toBe('timeout');
    });

    it('a Response with no req reaches onUnmatchedResponse', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const unmatched: Data<'response'>[] = [];
      socket.onUnmatchedResponse((d) => unmatched.push(d));
      ws.push('response', { ok: false, code: 'rate_limited', message: 'slow down' });
      ws.push('response', { ok: true }, { req: 'r-unknown' });
      expect(unmatched).toMatchObject([
        { ok: false, code: 'rate_limited', message: 'slow down' },
        { ok: true, code: '', message: '' },
      ]);
    });

    it('a request pending across a reconnect is re-sent once with a new req and the same idempotency key', async () => {
      const socket = newSocket({ requestTimeoutMs: 60_000 });
      const first = await connected(socket);
      const p = socket.request('inventoryDiscard', {
        idempotencyKey: 'k-7',
        itemId: 'i1',
        quantity: 1,
      });
      const before = first.frames().at(-1);
      first.closeFromServer(4002, 'no pong');
      await vi.advanceTimersByTimeAsync(100);
      const second = FakeWebSocket.latest();
      expect(second).not.toBe(first);
      second.open();
      second.push('welcome', welcome);
      const again = second.frames().find((f) => f.t === 'inventoryDiscard');
      expect(again).toBeDefined();
      expect(again?.req).not.toBe(before?.req);
      expect(again?.d).toMatchObject({ idempotencyKey: 'k-7', itemId: 'i1', quantity: 1 });
      second.push('response', { ok: true }, { req: again?.req ?? '' });
      await expect(p).resolves.toMatchObject({ ok: true });
      // A second drop while still pending rejects instead of a third send.
      const q = socket.request('partyLeave', {});
      const rejected = q.catch((e: RequestError) => e.code);
      second.closeFromServer(4001);
      await vi.advanceTimersByTimeAsync(100);
      const third = FakeWebSocket.latest();
      third.open();
      third.push('welcome', welcome);
      third.closeFromServer(4001);
      await vi.advanceTimersByTimeAsync(200);
      const fourth = FakeWebSocket.latest();
      fourth.open();
      fourth.push('welcome', welcome);
      expect(await rejected).toBe('disconnected');
    });
  });

  describe('inputs and actions', () => {
    it('one Input per tick goes out in sequence order; a second in the same tick is refused', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      expect(socket.sendInput({ ...noInput, right: true }, 1)).toBe(true);
      expect(socket.sendInput({ ...noInput, right: true, jump: true }, 1)).toBe(false);
      expect(socket.sendInput({ ...noInput, left: true }, 2)).toBe(true);
      expect(socket.sendInput(noInput, 3)).toBe(true);
      const inputs = ws.frames().filter((f) => f.t === 'input');
      expect(inputs).toMatchObject([
        { seq: 1, d: { ...noInput, right: true } },
        { seq: 2, d: { ...noInput, left: true } },
        { seq: 3, d: noInput },
      ]);
    });

    it('actions carry the current sequence number right away', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      socket.sendInput({ ...noInput, right: true }, 1);
      expect(socket.sendAction('attack')).toBe(true);
      socket.sendAction('pickUp');
      socket.sendAction('enterPortal');
      expect(ws.frames().slice(-3)).toMatchObject([
        { t: 'attack', seq: 1 },
        { t: 'pickUp', seq: 1 },
        { t: 'enterPortal', seq: 1 },
      ]);
      expect(socket.sequence).toBe(1);
    });

    it('nothing is sent while not connected', () => {
      const socket = newSocket();
      expect(socket.sendInput(noInput, 1)).toBe(false);
      expect(socket.sendAction('attack')).toBe(false);
      expect(socket.send('chatSend', { channel: ChatChannel.MAP, text: 'hi' })).toBe(false);
    });
  });

  describe('subscriptions', () => {
    it('typed handlers see their message and can unsubscribe', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const seen: { tick: number; ackSeq: number }[] = [];
      const off = socket.on('delta', (m) => seen.push({ tick: m.tick, ackSeq: m.d.ackSeq }));
      ws.push('delta', { ackSeq: 7 }, { tick: 1 });
      off();
      ws.push('delta', { ackSeq: 8 }, { tick: 2 });
      expect(seen).toEqual([{ tick: 1, ackSeq: 7 }]);
    });

    it('an unknown frame from the server is reported, not thrown', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const errors: ProtocolError[] = [];
      socket.onProtocolError((e) => errors.push(e));
      expect(() => ws.pushRaw(new Uint8Array([0xaa, 0x01, 0x00]).buffer)).not.toThrow(); // field 21
      expect(() => ws.pushRaw(new Uint8Array([0xff]).buffer)).not.toThrow();
      expect(() => ws.pushRaw('a text frame')).not.toThrow();
      expect(() => ws.pushRaw(new Blob())).not.toThrow();
      expect(errors.map((e) => e.message)).toEqual([
        'no body, or one this client does not know',
        expect.stringMatching(/^bad frame/),
        'a text frame on the binary codec',
        'frame is neither text nor binary',
      ]);
      expect(socket.currentStatus).toBe('connected');
    });

    it('a Pong gives the round-trip time and the server time offset', async () => {
      let now = 1_000_000;
      const socket = newSocket({ now: () => now, pingIntervalMs: 15_000 });
      const ws = await connected(socket);
      await vi.advanceTimersByTimeAsync(15_000);
      const ping = ws.frames().at(-1);
      expect(ping?.t).toBe('ping');
      expect(ping?.d).toMatchObject({ clientTime: 1_000_000n });
      now += 80;
      ws.push('pong', { clientTime: 1_000_000n, serverTime: 5_000_040n });
      expect(socket.rtt).toBe(80);
      expect(socket.serverTimeOffset).toBe(5_000_040 - (1_000_080 - 40));
    });

    it('a silent connection is dropped after the pong deadline and comes back', async () => {
      const socket = newSocket({ pingIntervalMs: 15_000, pongTimeoutMs: 5000 });
      const ws = await connected(socket);
      await vi.advanceTimersByTimeAsync(15_000 + 5000);
      expect(ws.closedWith?.reason).toBe('no pong');
      expect(socket.currentStatus).toBe('reconnecting');
    });
  });

  describe('closes', () => {
    it.each([
      { name: 'close 4002 (no pong)', code: 4002, kick: undefined },
      { name: 'close 4001 (slow consumer)', code: 4001, kick: undefined },
      { name: 'a dropped connection', code: 1006, kick: undefined },
      { name: 'Kicked server_restart then 4003', code: 4003, kick: KickReason.SERVER_RESTART },
    ])('$name reconnects with backoff and a new Hello', async ({ code, kick }) => {
      const socket = newSocket({ backoff: { baseMs: 100, capMs: 1000, random: () => 0.999 } });
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const first = await connected(socket);
      if (kick) {
        first.push('kicked', { reason: kick });
      }
      first.closeFromServer(code);
      expect(socket.currentStatus).toBe('reconnecting');
      await vi.advanceTimersByTimeAsync(98);
      expect(FakeWebSocket.instances).toHaveLength(1); // backoff(0) is just under 100 ms
      await vi.advanceTimersByTimeAsync(2);
      const second = FakeWebSocket.latest();
      expect(second).not.toBe(first);
      second.open();
      expect(second.frames()[0]).toMatchObject({
        t: 'hello',
        d: { token: 'tok-login-rotated', protocolVersion: 1, characterId: 'c1' },
      });
      second.push('welcome', { ...welcome, token: 'tok-rotated-2' });
      expect(socket.currentStatus).toBe('connected');
      expect(getToken()).toBe('tok-rotated-2');
      expect(statuses.map((s) => s.status)).toEqual([
        'connecting',
        'connected',
        'reconnecting',
        'reconnecting',
        'connected',
      ]);
    });

    it('a second failure backs off longer, and reconnect never runs twice at once', async () => {
      const socket = newSocket({ backoff: { baseMs: 100, capMs: 1000, random: () => 0.999 } });
      const first = await connected(socket);
      first.closeFromServer(4002);
      await vi.advanceTimersByTimeAsync(100);
      const second = FakeWebSocket.latest();
      second.open();
      second.closeFromServer(4002); // fails before Welcome
      await vi.advanceTimersByTimeAsync(100);
      expect(FakeWebSocket.instances).toHaveLength(2); // backoff(1) is just under 200 ms
      await vi.advanceTimersByTimeAsync(100);
      expect(FakeWebSocket.instances).toHaveLength(3);
    });

    it.each([
      {
        name: 'Kicked unauthenticated then 4003',
        kick: KickReason.UNAUTHENTICATED,
        reason: 'session_ended',
        tokenAfter: null,
      },
      {
        name: 'Kicked session_expired then 4003',
        kick: KickReason.SESSION_EXPIRED,
        reason: 'session_ended',
        tokenAfter: null,
      },
      {
        name: 'Kicked logged_in_elsewhere then 4003',
        kick: KickReason.LOGGED_IN_ELSEWHERE,
        reason: 'kicked',
        tokenAfter: 'tok-login-rotated',
      },
      {
        name: 'Kicked abuse then 4003',
        kick: KickReason.ABUSE,
        reason: 'kicked',
        tokenAfter: 'tok-login-rotated',
      },
      {
        name: 'Kicked outdated_protocol then 4003',
        kick: KickReason.OUTDATED_PROTOCOL,
        reason: 'outdated_protocol',
        tokenAfter: 'tok-login-rotated',
      },
    ])('$name closes for good', async ({ kick, reason, tokenAfter }) => {
      const socket = newSocket();
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const ws = await connected(socket);
      const pending = socket.request('partyLeave', {}).catch((e: RequestError) => e.code);
      ws.push('kicked', { reason: kick });
      ws.closeFromServer(4003);
      expect(socket.currentStatus).toBe('closed');
      expect(statuses.at(-1)).toMatchObject({ status: 'closed', reason, kick, code: 4003 });
      expect(getToken()).toBe(tokenAfter);
      expect(await pending).toBe('disconnected');
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('a 4003 with no Kicked, or a reason this client does not know, is a kick', async () => {
      const socket = newSocket();
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const ws = await connected(socket);
      ws.closeFromServer(4003);
      expect(statuses.at(-1)).toEqual({ status: 'closed', reason: 'kicked', code: 4003 });
      const again = newSocket();
      again.onStatus((e) => statuses.push(e));
      const next = await connected(again);
      next.push('kicked', { reason: 99 as KickReason });
      next.closeFromServer(4003);
      expect(statuses.at(-1)).toEqual({ status: 'closed', reason: 'kicked', kick: 99, code: 4003 });
    });

    it('close 1008 (no subprotocol the server speaks) is an outdated client', async () => {
      const socket = newSocket();
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const ws = await connected(socket);
      const pending = socket.request('partyLeave', {}).catch((e: RequestError) => e.code);
      ws.closeFromServer(1008, 'ask for a subprotocol: finga.v1.proto, finga.v1.json');
      expect(statuses.at(-1)).toEqual({
        status: 'closed',
        reason: 'outdated_protocol',
        code: 1008,
      });
      expect(await pending).toBe('disconnected');
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('close 4004 (bad message) does not reconnect', async () => {
      const socket = newSocket();
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const ws = await connected(socket);
      ws.closeFromServer(4004, 'bad message');
      expect(statuses.at(-1)).toEqual({ status: 'closed', reason: 'bad_message', code: 4004 });
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('close() by the user ends it, rejects pending requests, and nothing reconnects', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const pending = socket.request('partyLeave', {}).catch((e: RequestError) => e.code);
      socket.close();
      expect(ws.closedWith).toEqual({ code: 1000, reason: 'bye' });
      expect(socket.currentStatus).toBe('closed');
      expect(await pending).toBe('disconnected');
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
      expect(getToken()).toBe('tok-login-rotated'); // logging out is the REST client's job
    });
  });
});
