import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { jsonCodec } from '@/protocol/codec';
import { ERR } from '@/protocol/errors';
import { ChatChannel, KickReason, WelcomeSchema } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { Input } from '@/sim/input';
import { FakeWebSocket } from '@/testing/fake-socket';

import { GameSocket, PROTOCOL_VERSION, RequestError } from './socket';
import type { SocketOptions, SocketStatus, StatusEvent } from './socket';
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
  },
  mapId: 'town',
  tick: 10,
  serverTime: 1_761_000_000_050n,
});

const latest = new WeakMap<GameSocket, SocketStatus>();

function newSocket(overrides: Partial<SocketOptions> = {}): GameSocket {
  const socket = new GameSocket({ url: 'ws://game.test/ws', ...overrides });
  socket.onStatus((e) => latest.set(socket, e.status));
  return socket;
}

/** Its status as the last onStatus event gave it. */
const statusOf = (socket: GameSocket) => latest.get(socket);

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
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.spyOn(Math, 'random').mockReturnValue(0.5); // the reconnect jitter: backoff(0) is 250 ms, backoff(1) 500 ms
    FakeWebSocket.reset();
  });
  afterEach(() => {
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
    expect(statusOf(socket)).toBe('connected');
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
    expect(statusOf(socket)).toBe('connected');
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
    const socket = newSocket();
    setToken('tok-login');
    const p = socket.connect('c1');
    const first = FakeWebSocket.latest();
    first.open();
    const rejected = p.catch((e: Error) => e.message);
    await vi.advanceTimersByTimeAsync(3999);
    expect(first.closedWith).toBeNull();
    await vi.advanceTimersByTimeAsync(1);
    expect(first.closedWith?.reason).toBe('no welcome');
    expect(await rejected).toMatch(/before Welcome/);
    expect(statusOf(socket)).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(250);
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
      ws.push('response', { ok: false, code: ERR.ETC_TAB_FULL }, { req: frame?.req ?? '' });
      const err = (await p.catch((e: unknown) => e)) as RequestError;
      expect(err).toBeInstanceOf(RequestError);
      expect(err.code).toBe(ERR.ETC_TAB_FULL);
      expect(err.message).toBe('Your Etc tab is full.');
    });

    it('a refusal with a code this client does not know keeps its number and names it', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const p = socket.request('partyLeave', {});
      ws.push('response', { ok: false, code: 220999 }, { req: ws.frames().at(-1)?.req ?? '' });
      const err = (await p.catch((e: unknown) => e)) as RequestError;
      expect([err.code, err.message]).toEqual([220999, 'Something went wrong (error 220999).']);
    });

    it('a refusal with no code is internal', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const p = socket.request('partyLeave', {});
      ws.push('response', { ok: false, code: 0 }, { req: ws.frames().at(-1)?.req ?? '' });
      const err = (await p.catch((e: unknown) => e)) as RequestError;
      expect([err.code, err.message]).toEqual([
        ERR.INTERNAL,
        'Something went wrong on the server. Try again.',
      ]);
    });

    it('times out when no Response comes', async () => {
      const socket = newSocket();
      await connected(socket);
      const p = socket.request('partyLeave', {});
      let code: RequestError['code'] | undefined;
      void p.catch((e: RequestError) => (code = e.code));
      await vi.advanceTimersByTimeAsync(4999);
      expect(code).toBeUndefined();
      await vi.advanceTimersByTimeAsync(1);
      expect(code).toBe('timeout');
    });

    it('a Response with no req reaches onUnmatchedResponse', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const unmatched: Data<'response'>[] = [];
      socket.onUnmatchedResponse((d) => unmatched.push(d));
      ws.push('response', { ok: false, code: ERR.RATE_LIMITED });
      ws.push('response', { ok: true }, { req: 'r-unknown' });
      expect(unmatched).toMatchObject([
        { ok: false, code: ERR.RATE_LIMITED },
        { ok: true, code: 0 },
      ]);
    });

    it('a request pending across a reconnect is re-sent once with a new req and the same idempotency key', async () => {
      const socket = newSocket();
      const first = await connected(socket);
      const p = socket.request('inventoryDiscard', {
        idempotencyKey: 'k-7',
        itemId: 'i1',
        quantity: 1,
      });
      const before = first.frames().at(-1);
      first.closeFromServer(4002, 'no pong');
      await vi.advanceTimersByTimeAsync(250);
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
      await vi.advanceTimersByTimeAsync(250);
      const third = FakeWebSocket.latest();
      third.open();
      third.push('welcome', welcome);
      third.closeFromServer(4001);
      await vi.advanceTimersByTimeAsync(500);
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
      expect(socket.sendInput({ ...noInput, right: true }, 1, 0)).toBe(true);
      expect(socket.sendInput({ ...noInput, right: true, jump: true }, 1, 0)).toBe(false);
      expect(socket.sendInput({ ...noInput, left: true }, 2, 0)).toBe(true);
      expect(socket.sendInput(noInput, 3, 77)).toBe(true);
      const inputs = ws.frames().filter((f) => f.t === 'input');
      expect(inputs).toMatchObject([
        { seq: 1, d: { ...noInput, right: true } },
        { seq: 2, d: { ...noInput, left: true } },
        { seq: 3, d: { ...noInput, view: 77 } }, // the tick of the mobs drawn as it was predicted
      ]);
    });

    it('actions carry the current sequence number right away', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      socket.sendInput({ ...noInput, right: true }, 1, 0);
      expect(socket.sendAction('attack')).toBe(true);
      socket.sendAction('enterPortal');
      expect(ws.frames().slice(-2)).toMatchObject([
        { t: 'attack', seq: 1 },
        { t: 'enterPortal', seq: 1 },
      ]);
      expect(socket.sequence).toBe(1);
    });

    it('nothing is sent while not connected', () => {
      const socket = newSocket();
      expect(socket.sendInput(noInput, 1, 0)).toBe(false);
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
      vi.mocked(console.info).mockClear();
      expect(() => ws.pushRaw(new Uint8Array([0x9a, 0x06, 0x00]).buffer)).not.toThrow(); // field 99
      expect(() => ws.pushRaw(new Uint8Array([0xff]).buffer)).not.toThrow();
      expect(() => ws.pushRaw('a text frame')).not.toThrow();
      expect(() => ws.pushRaw(new Blob())).not.toThrow();
      expect(vi.mocked(console.info).mock.calls).toEqual([
        [
          'finga: dropped a frame this client cannot read: no body, or one this client does not know',
        ],
        [expect.stringMatching(/^finga: dropped a frame this client cannot read: bad frame/)],
        ['finga: dropped a frame this client cannot read: a text frame on the binary codec'],
        ['finga: dropped a frame this client cannot read: frame is neither text nor binary'],
      ]);
      expect(statusOf(socket)).toBe('connected');
    });

    it('a Pong gives the round-trip time and the server time offset', async () => {
      vi.setSystemTime(985_000);
      const socket = newSocket();
      const ws = await connected(socket);
      await vi.advanceTimersByTimeAsync(15_000);
      const ping = ws.frames().at(-1);
      expect(ping?.t).toBe('ping');
      expect(ping?.d).toMatchObject({ clientTime: 1_000_000n });
      vi.setSystemTime(1_000_080);
      ws.push('pong', { clientTime: 1_000_000n, serverTime: 5_000_040n });
      expect(socket.rtt).toBe(80);
      expect(socket.serverTimeOffset).toBe(5_000_040 - (1_000_080 - 40));
    });

    it('a silent connection is dropped after the pong deadline and comes back', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      await vi.advanceTimersByTimeAsync(15_000 + 4999);
      expect(ws.closedWith).toBeNull();
      await vi.advanceTimersByTimeAsync(1);
      expect(ws.closedWith?.reason).toBe('no pong');
      expect(statusOf(socket)).toBe('reconnecting');
    });
  });

  describe('closes', () => {
    it.each([
      { name: 'close 4002 (no pong)', code: 4002, kick: undefined },
      { name: 'close 4001 (slow consumer)', code: 4001, kick: undefined },
      { name: 'a dropped connection', code: 1006, kick: undefined },
      { name: 'Kicked server_restart then 4003', code: 4003, kick: KickReason.SERVER_RESTART },
    ])('$name reconnects with backoff and a new Hello', async ({ code, kick }) => {
      vi.mocked(Math.random).mockReturnValue(0.999);
      const socket = newSocket();
      const statuses: StatusEvent[] = [];
      socket.onStatus((e) => statuses.push(e));
      const first = await connected(socket);
      if (kick) {
        first.push('kicked', { reason: kick });
      }
      first.closeFromServer(code);
      expect(statusOf(socket)).toBe('reconnecting');
      await vi.advanceTimersByTimeAsync(498);
      expect(FakeWebSocket.instances).toHaveLength(1); // backoff(0) is just under 500 ms
      await vi.advanceTimersByTimeAsync(1);
      const second = FakeWebSocket.latest();
      expect(second).not.toBe(first);
      second.open();
      expect(second.frames()[0]).toMatchObject({
        t: 'hello',
        d: { token: 'tok-login-rotated', protocolVersion: 1, characterId: 'c1' },
      });
      second.push('welcome', { ...welcome, token: 'tok-rotated-2' });
      expect(statusOf(socket)).toBe('connected');
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
      vi.mocked(Math.random).mockReturnValue(0.999);
      const socket = newSocket();
      const first = await connected(socket);
      first.closeFromServer(4002);
      await vi.advanceTimersByTimeAsync(499);
      const second = FakeWebSocket.latest();
      second.open();
      second.closeFromServer(4002); // fails before Welcome
      await vi.advanceTimersByTimeAsync(998);
      expect(FakeWebSocket.instances).toHaveLength(2); // backoff(1) is just under 1000 ms
      await vi.advanceTimersByTimeAsync(1);
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
      expect(statusOf(socket)).toBe('closed');
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

    it('writes each close to the console, so a drop reads apart from the server closing', async () => {
      const ws = await connected(newSocket());
      ws.closeFromServer(1006);
      expect(console.info).toHaveBeenLastCalledWith(
        'finga: the connection closed with 1006, not clean',
      );
      await vi.advanceTimersByTimeAsync(1000);
      FakeWebSocket.latest().open();
      FakeWebSocket.latest().push('welcome', welcome);
      await vi.advanceTimersByTimeAsync(0);
      FakeWebSocket.latest().closeFromServer(4003, 'kicked');
      expect(console.info).toHaveBeenLastCalledWith(
        'finga: the connection closed with 4003 (kicked)',
      );
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

    it('close() during a pending retry cancels it, so a connect right after opens exactly one socket', async () => {
      const socket = newSocket();
      const first = await connected(socket);
      first.closeFromServer(1006);
      expect(statusOf(socket)).toBe('reconnecting');
      socket.close();
      const p = socket.connect('c1');
      const second = FakeWebSocket.latest();
      second.open();
      second.push('welcome', welcome);
      await p;
      await vi.advanceTimersByTimeAsync(1_000); // past the retry's 250 ms
      expect(FakeWebSocket.instances).toEqual([first, second]);
      expect(statusOf(socket)).toBe('connected');
    });

    it('close() while still connecting closes that socket too, and nothing reconnects', async () => {
      const socket = newSocket();
      setToken('tok-login');
      void socket.connect('c1'); // left pending: the caller has moved on
      const ws = FakeWebSocket.latest();
      socket.close();
      expect(ws.closedWith).toEqual({ code: 1000, reason: 'bye' });
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('a socket replaced before it opens sends no Hello on the one that replaced it', () => {
      const socket = newSocket();
      setToken('tok-login');
      void socket.connect('c1');
      const first = FakeWebSocket.latest();
      void socket.connect('c1');
      const second = FakeWebSocket.latest();
      second.open();
      first.open();
      expect(second.frames().map((f) => f.t)).toEqual(['hello']);
    });

    it('close() by the user ends it, rejects pending requests, and nothing reconnects', async () => {
      const socket = newSocket();
      const ws = await connected(socket);
      const pending = socket.request('partyLeave', {}).catch((e: RequestError) => e.code);
      socket.close();
      expect(ws.closedWith).toEqual({ code: 1000, reason: 'bye' });
      expect(statusOf(socket)).toBe('closed');
      expect(await pending).toBe('disconnected');
      await vi.advanceTimersByTimeAsync(10_000);
      expect(FakeWebSocket.instances).toHaveLength(1);
      expect(getToken()).toBe('tok-login-rotated'); // logging out is the REST client's job
    });
  });
});
