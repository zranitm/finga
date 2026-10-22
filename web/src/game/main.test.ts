import { beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { FakeGame } from '@/testing/fake-phaser';
import { fakeWebSocketClass } from '@/testing/fake-socket';

import { SCENES } from './keys';
import { GameHandle } from './main';
import { REGISTRY } from './registry';

vi.mock('phaser', async () => (await import('@/testing/fake-phaser')).fakePhaser());

const world = { mapId: 'town', characterId: 'c-ayla', cls: 'warrior', name: 'Ayla' };

describe('GameHandle', () => {
  let socket: GameSocket;
  beforeEach(() => {
    FakeGame.reset();
    socket = new GameSocket({ url: 'ws://game.test/ws', WebSocket: fakeWebSocketClass });
  });

  it('attach creates one game in the parent with the socket in the registry, and the cleanup destroys it', () => {
    const handle = new GameHandle({ socket });
    const parent = document.createElement('div');
    const detach = handle.attach(parent);
    expect(FakeGame.live()).toHaveLength(1);
    expect(FakeGame.instances[0]?.config.parent).toBe(parent);
    expect(FakeGame.instances[0]?.registry.get(REGISTRY.socket)).toBe(socket);
    expect(handle.attached).toBe(true);
    detach();
    expect(FakeGame.live()).toHaveLength(0);
    expect(handle.attached).toBe(false);
  });

  it('a double attach, as in React development, leaves one live game', () => {
    const handle = new GameHandle({ socket });
    const parent = document.createElement('div');
    const first = handle.attach(parent);
    first();
    handle.attach(parent);
    expect(FakeGame.instances).toHaveLength(2);
    expect(FakeGame.live()).toHaveLength(1);
  });

  it('enter before attach is kept and starts the World once the game exists', () => {
    const handle = new GameHandle({ socket });
    handle.enter(world);
    handle.attach(document.createElement('div'));
    expect(FakeGame.instances[0]?.started).toEqual([[SCENES.World, world]]);
  });

  it.each([
    {
      name: 'enter while the World runs restarts it with the new data',
      act: (h: GameHandle) => {
        h.enter(world);
        h.enter({ ...world, mapId: 'forest' });
      },
      started: [
        [SCENES.World, world],
        [SCENES.World, { ...world, mapId: 'forest' }],
      ],
      stopped: [SCENES.World],
    },
    {
      name: 'leave stops the World',
      act: (h: GameHandle) => {
        h.enter(world);
        h.leave();
      },
      started: [[SCENES.World, world]],
      stopped: [SCENES.World],
    },
    {
      name: 'leave after enter before attach starts nothing',
      act: (h: GameHandle) => {
        h.leave();
      },
      started: [],
      stopped: [SCENES.World],
    },
  ])('$name', ({ act, started, stopped }) => {
    const handle = new GameHandle({ socket });
    handle.attach(document.createElement('div'));
    act(handle);
    expect(FakeGame.instances[0]?.started).toEqual(started);
    expect(FakeGame.instances[0]?.stopped).toEqual(stopped);
  });
});
