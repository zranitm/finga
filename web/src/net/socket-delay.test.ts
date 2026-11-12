import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Data } from '@/protocol/messages';
import type { Input } from '@/sim/input';
import { FakeWebSocket } from '@/testing/fake-socket';

import { GameSocket } from './socket';
import { clearToken, setToken } from './token';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

const welcome = { token: 'tok-rotated', character: { id: 'c-ayla', name: 'Ayla' }, mapId: 'town' };

describe('GameSocket delayMs', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    setToken('tok-login');
  });
  afterEach(() => {
    vi.useRealTimers();
    clearToken();
  });

  it('holds frames both ways for the delay, and no delay means at once', async () => {
    const socket = new GameSocket({
      url: 'ws://game.test/ws',
      delayMs: 100,
    });
    const statuses: string[] = [];
    socket.onStatus((e) => statuses.push(e.status));
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    expect(ws.frames()).toHaveLength(0); // Hello waits
    await vi.advanceTimersByTimeAsync(100);
    expect(ws.frames()[0]?.t).toBe('hello');

    ws.push('welcome', welcome);
    expect(statuses.at(-1)).toBe('connecting'); // Welcome waits too
    await vi.advanceTimersByTimeAsync(100);
    await p;
    expect(statuses.at(-1)).toBe('connected');

    const seen: Data<'snapshot'>[] = [];
    socket.on('snapshot', (m) => seen.push(m.d));
    ws.push('snapshot', {});
    socket.sendInput({ ...noInput, right: true }, 1);
    await vi.advanceTimersByTimeAsync(99);
    expect(seen).toHaveLength(0);
    expect(ws.frames().filter((f) => f.t === 'input')).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(seen).toHaveLength(1);
    expect(ws.frames().filter((f) => f.t === 'input')).toHaveLength(1);
  });

  it('a frame delayed past the socket closing is dropped, not sent on the next one', async () => {
    const socket = new GameSocket({
      url: 'ws://game.test/ws',
      delayMs: 50,
    });
    vi.spyOn(Math, 'random').mockReturnValue(0); // the next socket opens at once
    const p = socket.connect('c-ayla');
    const ws = FakeWebSocket.latest();
    ws.open();
    await vi.advanceTimersByTimeAsync(50);
    ws.push('welcome', welcome);
    await vi.advanceTimersByTimeAsync(50);
    await p;
    socket.sendInput({ ...noInput, left: true }, 1);
    ws.closeFromServer(4002); // before the delayed Input leaves
    await vi.advanceTimersByTimeAsync(50);
    expect(ws.frames().filter((f) => f.t === 'input')).toHaveLength(0);
  });
});
