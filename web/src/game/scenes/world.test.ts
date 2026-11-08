import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, setToken } from '@/net/token';
import { WelcomeSchema } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import { character } from '@/state/character';
import { SceneRunner } from '@/testing/fake-phaser';
import type { FakeScene } from '@/testing/fake-phaser';
import { FakeWebSocket, fakeWebSocketClass } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import { Arrival } from '../systems/arrival';

import { World } from './world';
import type { WorldData } from './world';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

const MAPS = ['town', 'forest', 'cave'];
const ME = 'c-ayla';
const RATE = 20;
const FRAME_MS = 50;

const mapJson = (id: string): unknown =>
  JSON.parse(readFileSync(path.resolve(process.cwd(), `public/maps/${id}.tmj`), 'utf8'));

function snapshotOn(mapId: string): ServerInit<'snapshot'> {
  const { x, y } = parseMap(mapId, mapJson(mapId)).player_spawn;
  return {
    players: [{ id: ME, name: 'Ayla', class: 'warrior', x, y, facing: 1, onGround: true, hp: 50 }],
  };
}

/** Counts the socket's subscriptions still held, whoever holds them. */
function countListeners(socket: GameSocket): () => number {
  let live = 0;
  const on = socket.on.bind(socket);
  vi.spyOn(socket, 'on').mockImplementation((t, handler) => {
    live++;
    const off = on(t, handler);
    let done = false;
    return () => {
      if (!done) {
        done = true;
        live--;
      }
      off();
    };
  });
  return () => live;
}

async function connected(mapId: string) {
  const socket = new GameSocket({
    url: 'ws://game.test/ws',
    WebSocket: fakeWebSocketClass,
    backoff: { baseMs: 10, capMs: 10, random: () => 0 },
  });
  const listeners = countListeners(socket);
  setToken('tok-login');
  const welcomed = socket.connect(ME);
  FakeWebSocket.latest().open();
  FakeWebSocket.latest().push('welcome', { token: 'tok-1', mapId, tickRateHz: RATE });
  await welcomed;
  return { socket, listeners };
}

/** A World scene entered on mapId, as the game enters it after Welcome. */
function entered(socket: GameSocket, mapId: string, assetsReady = true) {
  const scene = new World();
  const fake = scene as unknown as FakeScene;
  for (const id of MAPS) {
    fake.cache.json.set(mapKey(id), mapJson(id));
  }
  fake.registry.set(REGISTRY.socket, socket);
  fake.registry.set(REGISTRY.assetsReady, assetsReady);
  fake.registry.set(REGISTRY.visit, 1);
  const built: string[] = [];
  const get = fake.cache.json.get.bind(fake.cache.json);
  vi.spyOn(fake.cache.json, 'get').mockImplementation((key) => {
    built.push(key);
    return get(key);
  });
  const runner = new SceneRunner(scene);
  const data: WorldData = {
    characterId: ME,
    cls: 'warrior',
    name: 'Ayla',
    visit: 1,
    arriving: new Arrival(socket, mapId, RATE),
  };
  runner.start(data);
  let now = 0;
  const frames = (n: number) => {
    for (let i = 0; i < n; i++) {
      now += FRAME_MS;
      runner.step(now, FRAME_MS);
    }
  };
  const inputs = () =>
    FakeWebSocket.latest()
      .frames()
      .filter((f) => f.t === 'input').length;
  const assetsLoaded = () => {
    fake.registry.set(REGISTRY.assetsReady, true);
    fake.game.events.emit(EVENTS.assetsReady);
  };
  const leave = () => {
    fake.registry.set(REGISTRY.visit, 2); // as the game's leave: this visit is over
    runner.stop();
  };
  return { runner, built, frames, inputs, assetsLoaded, leave };
}

/** A World scene on a real socket, entered on mapId and placed by its first Snapshot. */
async function playing(mapId: string) {
  const { socket, listeners } = await connected(mapId);
  const world = entered(socket, mapId);
  FakeWebSocket.latest().push('snapshot', snapshotOn(mapId), { tick: 1 });
  world.frames(3);
  expect(world.inputs()).toBeGreaterThan(0);
  return { ...world, listeners, ownListeners: listeners() };
}

describe('World', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.reset();
    resetStores();
  });
  afterEach(() => {
    vi.useRealTimers();
    clearToken();
  });

  it.each([
    { name: 'another map', mapId: 'town', tickRateHz: RATE },
    { name: 'another tick rate', mapId: 'forest', tickRateHz: 30 },
  ])(
    'a reconnect onto $name, its Snapshot right behind the Welcome, places the player there',
    async ({ mapId, tickRateHz }) => {
      const { built, frames, inputs, listeners, ownListeners } = await playing('forest');
      FakeWebSocket.latest().closeFromServer(1006);
      vi.advanceTimersByTime(10);
      const ws = FakeWebSocket.latest();
      ws.open();
      ws.push('welcome', { token: 'tok-2', mapId, tickRateHz });
      ws.push('snapshot', snapshotOn(mapId), { tick: 900 }); // before the restart runs, on the next step
      frames(3);
      expect(built.at(-1)).toBe(mapKey(mapId));
      expect(inputs()).toBeGreaterThan(0); // a placed player sends Input; an unplaced one never does
      expect(listeners()).toBe(ownListeners);
    },
  );

  it('a Snapshot that lands while the assets still load places the player once they are in', async () => {
    const { socket } = await connected('forest');
    const { built, frames, inputs, assetsLoaded } = entered(socket, 'forest', false);
    FakeWebSocket.latest().push('snapshot', snapshotOn('forest'), { tick: 1 });
    assetsLoaded();
    frames(3);
    expect(built).toEqual([mapKey('forest')]);
    expect(inputs()).toBeGreaterThan(0);
  });

  it('a scene stopped during the map change’s fade lets go of the socket', async () => {
    const { runner, listeners } = await playing('town');
    FakeWebSocket.latest().push('mapChange', { mapId: 'forest', x: 100, y: 200 });
    expect(runner.camera.fading).toBe(true);
    runner.stop(); // back to characters, or a kick, before the fade ends
    expect(listeners()).toBe(0);
  });

  it('a restart queued as the app leaves the world builds nothing behind its screens', async () => {
    const { runner, built, frames, inputs, leave, listeners } = await playing('forest');
    FakeWebSocket.latest().closeFromServer(1006);
    vi.advanceTimersByTime(10);
    const ws = FakeWebSocket.latest();
    ws.open();
    ws.push('welcome', { token: 'tok-2', mapId: 'town', tickRateHz: RATE }); // queues a restart onto the town
    ws.push('snapshot', snapshotOn('town'), { tick: 900 });
    leave(); // back to characters before the next step runs it
    const sent = inputs();
    frames(3);
    expect(built).not.toContain(mapKey('town'));
    expect(runner.isRunning).toBe(false);
    expect(inputs()).toBe(sent);
    expect(listeners()).toBe(0);
  });

  it('a hit the Delta already carries is not taken again when its Damage follows, as the server sends them', async () => {
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        character: { id: ME, name: 'Ayla', class: 'warrior', level: 1, hp: 50, maxHp: 50 },
      }),
    );
    await playing('forest');
    const ws = FakeWebSocket.latest();
    const me = snapshotOn('forest').players![0]!;
    ws.push('delta', { players: [{ ...me, hp: 45, invulnerable: true }] }, { tick: 2 });
    ws.push('damage', { source: 'm:1', target: `p:${ME}`, amount: 5 }, { tick: 2 });
    expect(character.getState().character?.hp).toBe(45);
  });

  it('a second map change during the fade lands on the second map', async () => {
    const { runner, built, frames, inputs, listeners, ownListeners } = await playing('town');
    const ws = FakeWebSocket.latest();
    ws.push('mapChange', { mapId: 'forest', x: 100, y: 200 });
    ws.push('snapshot', snapshotOn('forest'), { tick: 2 });
    ws.push('mapChange', { mapId: 'cave', x: 300, y: 400 }); // a world crash, and a rejoin onto the map it saved
    ws.push('snapshot', snapshotOn('cave'), { tick: 3 });
    const sent = inputs();
    runner.camera.endFade();
    frames(3);
    expect(built.at(-1)).toBe(mapKey('cave'));
    expect(inputs()).toBeGreaterThan(sent);
    expect(listeners()).toBe(ownListeners);
  });
});
