// The development-only network delay (client step C08): every frame out
// and in waits delayMs, so a 100 ms delay is a 200 ms round trip.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { noInput } from '@/sim/input';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';

import { GameSocket } from './socket';
import { clearToken, setToken } from './token';

const welcome = {
  token: 'tok-rotated',
  character: {
    id: 'c-ayla',
    name: 'Ayla',
    class: 'warrior',
    level: 1,
    exp: 0,
    stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
    hp: 204,
    max_hp: 204,
    mp: 28,
    max_mp: 28,
  },
  inventory: [],
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

describe('GameSocket delayMs', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
    setToken('tok-login');
  });
  afterEach(() => {
    vi.useRealTimers();
    clearToken();
  });

  it('holds frames both ways for the delay, and no delay means at once', async () => {
    const socket = new GameSocket({
      url: 'ws://game.test/ws',
      WebSocket: fakeWebSocketClass,
      delayMs: 100,
    });
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    expect(ws.frames()).toHaveLength(0); // Hello waits
    await vi.advanceTimersByTimeAsync(100);
    expect(ws.frames()[0]?.t).toBe('Hello');

    ws.push({ t: 'Welcome', d: welcome });
    expect(socket.currentStatus).toBe('connecting'); // Welcome waits too
    await vi.advanceTimersByTimeAsync(100);
    await p;
    expect(socket.currentStatus).toBe('connected');

    const seen: unknown[] = [];
    socket.on('Snapshot', (m) => seen.push(m.d));
    ws.push({ t: 'Snapshot', d: { players: [], mobs: [], drops: [] } });
    socket.sendInput({ ...noInput, right: true }, 1);
    await vi.advanceTimersByTimeAsync(99);
    expect(seen).toHaveLength(0);
    expect(ws.frames().filter((f) => f.t === 'Input')).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(seen).toHaveLength(1);
    expect(ws.frames().filter((f) => f.t === 'Input')).toHaveLength(1);
  });

  it('a frame delayed past the socket closing is dropped, not sent on the next one', async () => {
    const socket = new GameSocket({
      url: 'ws://game.test/ws',
      WebSocket: fakeWebSocketClass,
      delayMs: 50,
      backoff: { baseMs: 10, capMs: 10 },
    });
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    await vi.advanceTimersByTimeAsync(50);
    ws.push({ t: 'Welcome', d: welcome });
    await vi.advanceTimersByTimeAsync(50);
    await p;
    socket.sendInput({ ...noInput, left: true }, 1);
    ws.closeFromServer(4002); // before the delayed Input leaves
    await vi.advanceTimersByTimeAsync(50);
    expect(ws.frames().filter((f) => f.t === 'Input')).toHaveLength(0);
  });
});
