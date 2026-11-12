import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, setToken } from '@/net/token';
import {
  FieldBossSchema,
  NpcDialogSchema,
  PromotedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import { character } from '@/state/character';
import { connection } from '@/state/connection';
import { fieldBoss } from '@/state/field-boss';
import { quests } from '@/state/quests';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { SceneRunner } from '@/testing/fake-phaser';
import type { FakeScene } from '@/testing/fake-phaser';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { PlayerView } from '../entities/player';
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
  const socket = new GameSocket({ url: 'ws://game.test/ws' });
  const listeners = countListeners(socket);
  setToken('tok-login');
  const welcomed = socket.connect(ME);
  FakeWebSocket.latest().open();
  FakeWebSocket.latest().push('welcome', { token: 'tok-1', mapId, tickRateHz: RATE });
  await welcomed;
  return { socket, listeners };
}

/** Every scene a test entered, stopped after it so none keeps the window's keys. */
const runners: SceneRunner[] = [];

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
  runners.push(runner);
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
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.spyOn(Math, 'random').mockReturnValue(0); // a dropped socket reconnects at once
    resetStores();
  });
  afterEach(() => {
    for (const r of runners.splice(0)) {
      if (r.isRunning) {
        r.stop();
      }
    }
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

  it("a rejoin's Welcome on the same map brings a dead player back: its Snapshot places them and input flows", async () => {
    const { frames, inputs, leave } = await playing('forest');
    const ws = FakeWebSocket.latest();
    ws.push('died', { characterId: ME, x: 480, y: 608 });
    frames(2);
    const dead = inputs();
    frames(3);
    expect(inputs()).toBe(dead); // the server holds a dead player
    ws.push('welcome', { token: '', mapId: 'forest', tickRateHz: RATE }); // the world restarted, and saved them alive
    ws.push('snapshot', snapshotOn('forest'), { tick: 900 });
    frames(3);
    expect(inputs()).toBeGreaterThan(dead);
    leave();
  });

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

  it('the field boss bar shows the HP of the boss as the scene draws it, and nothing once it is gone', async () => {
    const { frames } = await playing('forest');
    const ws = FakeWebSocket.latest();
    fieldBoss
      .getState()
      .applyFieldBoss(
        create(FieldBossSchema, { mobId: 9, type: 'king_slime', leavesInMs: 600000n }),
      );
    const king = { id: 9, type: 'king_slime', x: 1400, y: 608, facing: 1, hp: 4260, maxHp: 4260 };
    for (let tick = 2; tick <= 8; tick++) {
      ws.push('delta', { mobs: [{ ...king, hp: tick < 5 ? 4260 : 3100 }] }, { tick });
      frames(1);
    }
    frames(5);
    expect(fieldBoss.getState().hp).toEqual({ hp: 3100, maxHp: 4260 });
    ws.push('delta', { removedMobs: [9] }, { tick: 9 });
    for (let tick = 10; tick <= 20; tick++) {
      ws.push('delta', {}, { tick });
      frames(1);
    }
    expect(fieldBoss.getState().hp).toBeNull();
  });

  it('a world silent for two seconds holds the player still and takes no keys, until its frames come back', async () => {
    const { frames, inputs, leave } = await playing('forest');
    const ws = FakeWebSocket.latest();
    const sent = (t: string) => ws.frames().filter((f) => f.t === t).length;
    const key = (code: string, type: 'keydown' | 'keyup') =>
      window.dispatchEvent(new KeyboardEvent(type, { code, cancelable: true }));
    ws.push('delta', {}, { tick: 2 });
    frames(1);
    frames(40); // exactly two seconds of silence: input still flows
    expect(connection.getState().worldStalled).toBe(false);
    frames(1);
    expect(connection.getState().worldStalled).toBe(true);
    const held = inputs();
    key('ArrowRight', 'keydown');
    key('KeyX', 'keydown');
    frames(10);
    expect(inputs()).toBe(held); // nothing walks ahead of the server, so nothing snaps back
    expect(sent('attack')).toBe(0);
    ws.push('delta', {}, { tick: 3 });
    frames(2);
    expect(connection.getState().worldStalled).toBe(false);
    expect(inputs()).toBeGreaterThan(held);
    expect(sent('attack')).toBeGreaterThan(0); // the key still held strikes once the world is back
    key('ArrowRight', 'keyup');
    key('KeyX', 'keyup');
    leave();
    expect(connection.getState().worldStalled).toBe(false);
  });

  it('a stall the scene is left in is not shown on the next screen', async () => {
    const { frames, leave } = await playing('forest');
    frames(45);
    expect(connection.getState().worldStalled).toBe(true);
    leave();
    expect(connection.getState().worldStalled).toBe(false);
  });

  describe('NPCs', () => {
    const press = (code: string) => {
      window.dispatchEvent(new KeyboardEvent('keydown', { code, cancelable: true }));
      window.dispatchEvent(new KeyboardEvent('keyup', { code, cancelable: true }));
    };
    /** The player stands at x on the town's ground, as the server's Snapshot says. */
    const standAt = (x: number, tick: number) => {
      const me = snapshotOn('town').players![0]!;
      FakeWebSocket.latest().push('snapshot', { players: [{ ...me, x }] }, { tick });
    };
    const talks = () =>
      FakeWebSocket.latest()
        .frames()
        .filter((f) => f.t === 'talk');
    // Each scene binds the window's keys until it stops.
    let leave: (() => void) | undefined;
    const inTown = async () => {
      const world = await playing('town');
      leave = world.leave;
      return world;
    };
    afterEach(() => {
      leave?.();
      leave = undefined;
    });

    it('the Talk key talks to the guide within reach, and does nothing farther off', async () => {
      const { frames } = await inTown();
      standAt(566, 5); // 70 px from the guide at x 496
      frames(1);
      press('KeyZ');
      expect(talks()).toEqual([]);
      standAt(480, 6);
      frames(1);
      press('KeyZ');
      expect(talks()).toMatchObject([{ d: { npcId: 'town_guide' } }]);
    });

    it('the dialog closes once the player walks out of reach', async () => {
      const { frames } = await inTown();
      standAt(480, 5);
      frames(1);
      quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
      ui.getState().openWindow('npc');
      frames(2);
      expect(ui.getState().windows).toEqual(['npc']);
      standAt(600, 6);
      frames(2);
      expect(ui.getState().windows).toEqual([]);
    });

    it('the dialog closes when the player dies, and Talk does nothing while dead', async () => {
      const { frames } = await inTown();
      standAt(480, 5);
      frames(1);
      quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
      ui.getState().openWindow('npc');
      FakeWebSocket.latest().push('died', { characterId: ME, x: 480, y: 608 });
      expect(ui.getState().windows).toEqual([]);
      press('KeyZ');
      expect(talks()).toEqual([]);
      // A dialog answering a Talk from the tick the player died in arrives after the death.
      quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
      ui.getState().openWindow('npc');
      frames(1);
      expect(ui.getState().windows).toEqual([]);
    });

    it('Q opens and closes the quest log', async () => {
      await inTown();
      press('KeyQ');
      expect(ui.getState().windows).toEqual(['quests']);
      press('KeyQ');
      expect(ui.getState().windows).toEqual([]);
    });

    it('K opens and closes the skill window', async () => {
      await inTown();
      press('KeyK');
      expect(ui.getState().windows).toEqual(['skills']);
      press('KeyK');
      expect(ui.getState().windows).toEqual([]);
    });

    const uses = () =>
      FakeWebSocket.latest()
        .frames()
        .filter((f) => f.t === 'useSkill');
    /** Ayla with a point in Strike and 20 MP, and Strike on key 1. */
    const learned = () => {
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok-1',
          mapId: 'town',
          character: {
            id: ME,
            name: 'Ayla',
            class: 'warrior',
            level: 3,
            hp: 150,
            maxHp: 204,
            mp: 20,
            maxMp: 28,
            sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
          },
        }),
      );
      skillBar.getState().load(['strike']);
    };

    it('a skill key does nothing in a town for a damaging skill, and says why', async () => {
      await inTown();
      learned();
      press('Digit1');
      expect(uses()).toEqual([]);
      expect(toasts.getState().toasts.map((t) => t.text)).toEqual(['You can’t use that in town.']);
    });

    /** A forest with Strike learned, and the Attack frames sent; run plays frames and timers together, the world sending a frame a tick. */
    const fighting = async () => {
      const world = await playing('forest');
      leave = world.leave;
      learned();
      const attacks = () =>
        FakeWebSocket.latest()
          .frames()
          .filter((f) => f.t === 'attack').length;
      let tick = 2;
      const run = (ms: number) => {
        for (let t = 0; t < ms; t += FRAME_MS) {
          FakeWebSocket.latest().push('delta', {}, { tick: tick++ });
          world.frames(1);
          vi.advanceTimersByTime(FRAME_MS);
        }
      };
      return { attacks, run };
    };

    it('a skill key pressed early in an attack goes as it ends, ahead of the attack held meanwhile, which waits for the skill’s end', async () => {
      const { attacks, run } = await fighting();
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
      run(FRAME_MS);
      expect(attacks()).toBe(1);
      press('Digit1'); // well before the warrior's cancel point at 750 ms
      run(800);
      expect([uses().length, attacks()]).toEqual([0, 1]);
      run(100); // past the warrior's 900 ms
      expect([uses().length, attacks()]).toEqual([1, 1]);
      run(950); // Strike runs 1050 ms and can't be cut short
      expect(attacks()).toBe(1);
      run(100);
      expect(attacks()).toBe(2);
      window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyX', cancelable: true }));
    });

    it('a skill key pressed within the early press of an attack’s cancel point cuts the attack short there', async () => {
      const { attacks, run } = await fighting();
      press('KeyX');
      run(650);
      press('Digit1'); // 100 ms before the cancel point, 15 of the attack's 18 ticks
      run(50);
      expect([uses().length, attacks()]).toEqual([0, 1]);
      run(50);
      expect(uses().length).toBe(1);
    });

    it('with attack held, a fresh press cuts Strike short at its cancel point; the key held again after a release-all waits for its end', async () => {
      const key = (type: 'keydown' | 'keyup', code: string, init: KeyboardEventInit = {}) =>
        window.dispatchEvent(new KeyboardEvent(type, { code, cancelable: true, ...init }));
      for (const again of [false, true]) {
        const { attacks, run } = await fighting();
        key('keydown', 'KeyX');
        run(650);
        press('Digit1'); // Strike cuts the warrior's attack at 750 ms and runs to 1800, its cancel point at 1600
        run(900);
        expect([uses().length, attacks()]).toEqual([1, 1]);
        if (again) {
          key('keyup', 'MetaLeft', { key: 'Meta' }); // Cmd lets every key go
          key('keydown', 'KeyX', { repeat: true });
        } else {
          key('keyup', 'KeyX');
          key('keydown', 'KeyX');
        }
        run(100);
        expect(attacks()).toBe(again ? 1 : 2);
        run(200);
        expect(attacks()).toBe(2);
        key('keyup', 'KeyX');
        leave?.();
        leave = undefined;
      }
    });

    it('attack tapped while a skill press waits leaves it waiting: the skill goes at the attack’s end, as on main', async () => {
      const { attacks, run } = await fighting();
      press('KeyX');
      run(100);
      press('Digit1'); // too early to cut the warrior's attack: it waits for the end at 900 ms
      for (let i = 0; i < 7; i++) {
        run(100);
        press('KeyX');
      }
      expect([uses().length, attacks()]).toEqual([0, 1]);
      run(150);
      expect([uses().length, attacks()]).toEqual([1, 1]);
    });

    it('a held attack swings again right as its action ends, between frames', async () => {
      const world = await playing('forest');
      leave = world.leave;
      const attacks = () =>
        FakeWebSocket.latest()
          .frames()
          .filter((f) => f.t === 'attack').length;
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
      world.frames(1); // the held key's repeat is set for the attack's end
      vi.advanceTimersByTime(900); // the warrior's attack, with no frame since
      expect(attacks()).toBe(2);
      window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyX', cancelable: true }));
    });

    it('on a hunting map, a skill key uses the skill on its key once a cooldown, which outlives the scene, as the world carries it to the next map', async () => {
      const world = await playing('forest');
      leave = world.leave;
      learned();
      press('Digit1');
      press('Digit1');
      expect(uses()).toMatchObject([{ d: { skill: 'strike' } }]);
      expect(skillBar.getState().cooldowns.strike).toBeDefined();
      FakeWebSocket.latest().push('skillUsed', {
        caster: ME,
        skill: 'strike',
        stage: 0,
        facing: -1,
      });
      world.frames(1);
      world.leave();
      leave = undefined;
      expect(skillBar.getState().cooldowns.strike).toBeDefined();
    });

    it('an attack draws its beats and cues the cancel; Strike flashes its colour and, as an attack may cut it short, cues too', async () => {
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const cue = vi.spyOn(PlayerView.prototype, 'cueAt');
      const { run } = await fighting();
      press('KeyX');
      run(FRAME_MS);
      const [swing, flash] = act.mock.calls[0]!;
      expect(flash).toBeUndefined();
      expect(swing.cancelAt - swing.start).toBe(750); // the warrior's 900 ms, its cancel point at 15 of 18 ticks
      expect(cue).toHaveBeenLastCalledWith(swing.cancelAt - 100);
      run(900);
      press('Digit1');
      run(FRAME_MS);
      expect(act).toHaveBeenCalledTimes(2);
      expect(act.mock.calls[1]?.[1]).toBe(0xf0ead6); // Strike's physical colour
      expect(cue).toHaveBeenCalledTimes(2);
    });

    it("another player's skill draws on them: its beats from its arrival, and its flash in its colour", async () => {
      const world = await playing('forest');
      leave = world.leave;
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const me = snapshotOn('forest').players![0]!;
      const robin = { ...me, id: 'c-robin', name: 'Robin', x: (me.x ?? 0) + 100 };
      for (let tick = 2; tick <= 10; tick++) {
        FakeWebSocket.latest().push('delta', tick === 2 ? { players: [robin] } : {}, { tick });
        world.frames(1);
      }
      FakeWebSocket.latest().push('skillUsed', {
        caster: 'c-robin',
        skill: 'spark',
        stage: 0,
        facing: -1,
      });
      world.frames(1);
      expect(act).toHaveBeenCalledOnce();
      const [beats, flash] = act.mock.calls[0]!;
      expect(flash).toBe(0xb070f0);
      expect(beats.cancelAt - beats.start).toBe(850); // Spark's 21 ticks, the cancel point at 17
    });
  });

  it("a promotion re-skins the player and paces the attack at the new class's speed", async () => {
    const world = await playing('forest');
    const setClass = vi.spyOn(PlayerView.prototype, 'setClass');
    const attacks = () =>
      FakeWebSocket.latest()
        .frames()
        .filter((f) => f.t === 'attack').length;
    character
      .getState()
      .applyPromoted(
        create(PromotedSchema, { character: { id: ME, name: 'Ayla', class: 'thief', sheet: {} } }),
      );
    expect(setClass).toHaveBeenCalledWith('thief');
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
    for (let t = 0; t < 800; t += FRAME_MS) {
      world.frames(1);
      vi.advanceTimersByTime(FRAME_MS);
    }
    expect(attacks()).toBe(2); // the Thief's 750 ms, where the Warrior it entered as waits 900
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyX', cancelable: true }));
    world.leave();
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
