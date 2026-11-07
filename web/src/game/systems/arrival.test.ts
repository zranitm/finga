import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import type { Received } from '@/net/socket';
import {
  DeltaSchema,
  DiedSchema,
  RespawnedSchema,
  SnapshotSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import type { MessageType } from '@/protocol/messages';

import { Arrival } from './arrival';
import type { Feed } from './arrival';

interface TestFeed extends Feed {
  send<T extends MessageType>(m: Received<T>): void;
  listeners(): number;
}

/** feed stands in for the socket: send reaches whoever listens to the message's type. */
function feed(): TestFeed {
  const handlers = new Map<MessageType, Set<(m: never) => void>>();
  const f: TestFeed = {
    on(t, handler) {
      const set = handlers.get(t) ?? new Set();
      handlers.set(t, set);
      set.add(handler);
      return () => set.delete(handler);
    },
    send(m) {
      for (const h of handlers.get(m.t) ?? []) {
        h(m as never);
      }
    },
    listeners: () => [...handlers.values()].reduce((n, s) => n + s.size, 0),
  };
  return f;
}

const msg = <T extends MessageType>(t: T, d: Received<T>['d'], tick = 0): Received<T> => ({
  t,
  req: '',
  tick,
  d,
});
const snapshot = (tick: number) => msg('snapshot', create(SnapshotSchema, {}), tick);
const delta = (tick: number) => msg('delta', create(DeltaSchema, {}), tick);

describe('Arrival', () => {
  it('the Snapshot that lands during the fade, and what follows it, are handed over in order', () => {
    const f = feed();
    const a = new Arrival(f, 'forest', 20);
    f.send(snapshot(10));
    f.send(delta(11));
    const died = msg('died', create(DiedSchema, { characterId: 'c-ayla' }), 12);
    f.send(died);
    f.send(delta(13));
    expect(a.take().map((m) => [m.t, m.tick])).toEqual([
      ['snapshot', 10],
      ['delta', 11],
      ['died', 12],
      ['delta', 13],
    ]);
  });

  it('a Delta before the Snapshot is the old map’s, and is dropped', () => {
    const f = feed();
    const a = new Arrival(f, 'forest', 20);
    f.send(delta(9));
    f.send(snapshot(10));
    expect(a.take().map((m) => m.tick)).toEqual([10]);
  });

  it('after a death on another map, the town’s Snapshot following the MapChange is held', () => {
    const f = feed();
    const a = new Arrival(f, 'town', 20); // Died, Respawned and MapChange reached the old scene
    f.send(snapshot(40));
    expect(a.take().map((m) => [m.t, m.tick])).toEqual([['snapshot', 40]]);
    expect(a.mapId).toBe('town');
  });

  it('a reconnect during the fade starts over from its own Welcome and Snapshot', () => {
    const f = feed();
    const a = new Arrival(f, 'forest', 20);
    f.send(snapshot(10));
    f.send(msg('welcome', create(WelcomeSchema, { mapId: 'town', tickRateHz: 30 })));
    f.send(delta(11)); // before the reconnect's Snapshot
    f.send(snapshot(50));
    expect([a.mapId, a.tickRateHz, a.moved]).toEqual(['town', 30, true]);
    expect(a.take().map((m) => m.tick)).toEqual([50]);
  });

  it('take stops holding: nothing more is kept, and the socket is let go', () => {
    const f = feed();
    const a = new Arrival(f, 'forest', 20);
    expect(f.listeners()).toBe(6);
    f.send(snapshot(10));
    expect(a.take()).toHaveLength(1);
    expect(f.listeners()).toBe(0);
    f.send(snapshot(11));
    expect(a.take()).toEqual([]);
    expect(a.moved).toBe(false);
  });

  it('a respawn message for another player is held like any other, for the new scene to judge', () => {
    const f = feed();
    const a = new Arrival(f, 'forest', 20);
    f.send(snapshot(10));
    f.send(
      msg('respawned', create(RespawnedSchema, { characterId: 'c-robin', mapId: 'forest' }), 11),
    );
    expect(a.take().map((m) => m.t)).toEqual(['snapshot', 'respawned']);
  });
});
