import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { GameSocket } from '@/net/socket';
import { clearToken, setToken } from '@/net/token';
import { ERR } from '@/protocol/errors';
import {
  FieldBossSchema,
  MobPose,
  NpcDialogSchema,
  PartyStateSchema,
  PromotedSchema,
  StatsChangedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import { faster, params } from '@/sim/step';
import { character } from '@/state/character';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { party } from '@/state/party';
import { quests } from '@/state/quests';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { SceneRunner } from '@/testing/fake-phaser';
import type { FakeScene } from '@/testing/fake-phaser';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { resetStores } from '@/testing/reset-stores';

import { MobView } from '../entities/mob';
import { PlayerView } from '../entities/player';
import { mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import { Arrival } from '../systems/arrival';
import { DamageNumbers } from '../systems/combat-view';
import { heldTime } from '../systems/hit-stop';
import type { Hold } from '../systems/hit-stop';
import { Predictor } from '../systems/prediction';
import type { Pace } from '../systems/prediction';

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

/** Brings Robin, of the class given, 100 px past the player, into view: the deltas it takes to draw another player. Returns where Robin stands. */
function robinBeside(frames: (n: number) => void, cls = 'warrior'): { x: number; y: number } {
  const me = snapshotOn('forest').players![0]!;
  const robin = { ...me, id: 'c-robin', name: 'Robin', class: cls, x: (me.x ?? 0) + 100 };
  for (let tick = 2; tick <= 10; tick++) {
    FakeWebSocket.latest().push('delta', tick === 2 ? { players: [robin] } : {}, { tick });
    frames(1);
  }
  return { x: robin.x, y: robin.y ?? 0 };
}

/** The scene's private effect plays and taking a player's effect away, spied on to see what an event plays. */
type Drawing = Record<
  'attackLook' | 'effect' | 'cutShort' | 'afterimage' | 'playMobAttacks',
  (...args: unknown[]) => unknown
>;
/** What a caster's effect goes to, read from the scene that played it. */
function targetsOf(
  played: { mock: { contexts: unknown[] } },
  caster: string,
): string[] | undefined {
  const scene = played.mock.contexts.at(-1) as { effects: Map<string, { targets: string[] }> };
  return scene.effects.get(caster)?.targets;
}

/** The scene's private choice of a hit's damage type, spied on to see what a hit sparks in. */
type Sparks = Record<'damageFrom', (source: string) => string>;

/** A green slime 60 px past the player and a cave bat, whose bite is magic, 120 px past, drawn from the deltas at ticks from on. */
/** One slime, mob 9, at x on the forest's floor, drawn there by frames from tick from on. */
function slimeAt(frames: (n: number) => void, from: number, x: number): void {
  const me = snapshotOn('forest').players![0]!;
  const slime = { id: 9, type: 'green_slime', x, y: me.y ?? 0, facing: -1, hp: 34, maxHp: 34 };
  for (let tick = from; tick < from + 7; tick++) {
    FakeWebSocket.latest().push('delta', { mobs: [slime] }, { tick });
    frames(1);
  }
  frames(5);
}

function mobsBeside(frames: (n: number) => void, from: number): void {
  const me = snapshotOn('forest').players![0]!;
  const x = me.x ?? 0;
  const at = { y: me.y ?? 0, facing: -1, hp: 34, maxHp: 34 };
  const mobs = [
    { id: 1, type: 'green_slime', x: x + 60, ...at },
    { id: 2, type: 'cave_bat', x: x + 120, ...at },
  ];
  for (let tick = from; tick < from + 7; tick++) {
    FakeWebSocket.latest().push('delta', { mobs }, { tick });
    frames(1);
  }
  frames(5);
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
function entered(socket: GameSocket, mapId: string, assetsReady = true, cls = 'warrior') {
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
    cls,
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

/** A World scene on a real socket, entered on mapId as a cls and placed by its first Snapshot. */
async function playing(mapId: string, cls = 'warrior') {
  const { socket, listeners } = await connected(mapId);
  const world = entered(socket, mapId, true, cls);
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

  it('a mob drawn beside the player stops its walk where the world will, and each Input names the tick of the mobs drawn', async () => {
    const apply = vi.spyOn(PlayerView.prototype, 'apply');
    const { frames, leave } = await playing('forest');
    const ws = FakeWebSocket.latest();
    const me = snapshotOn('forest').players![0]!;
    const x = me.x ?? 0;
    const slime = {
      id: 9,
      type: 'green_slime',
      x: x + 60,
      y: me.y ?? 0,
      facing: -1,
      hp: 34,
      maxHp: 34,
    }; // 32 wide: its left side at x + 44
    for (let tick = 2; tick <= 12; tick++) {
      ws.push('delta', tick === 2 ? { mobs: [slime] } : {}, { tick });
      frames(1);
    }
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'ArrowRight', cancelable: true }));
    frames(20);
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'ArrowRight', cancelable: true }));
    expect(apply.mock.lastCall?.[0].x).toBe(x + 32); // stopped with its right side at the slime's left
    const views = ws
      .frames()
      .filter((f) => f.t === 'input')
      .map((f) => (f.d as { view?: number }).view ?? 0);
    expect(views.at(-1)).toBe(12); // no newer frame: held to the newest received
    expect(views.every((v, i) => i === 0 || v >= views[i - 1]!)).toBe(true);
    leave();
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
    /** Ayla with a point in Strike and full energy, and Strike on key 1. */
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
            sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
          },
        }),
      );
      energy.getState().welcome(10);
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

    it('a skill key pressed early in an attack goes as it ends, ahead of the attack held meanwhile, which goes at the skill’s cancel point', async () => {
      const { attacks, run } = await fighting();
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
      run(FRAME_MS);
      expect(attacks()).toBe(1);
      press('Digit1'); // well before the warrior's cancel point at 750 ms
      run(800);
      expect([uses().length, attacks()]).toEqual([0, 1]);
      run(100); // past the warrior's 900 ms
      expect([uses().length, attacks()]).toEqual([1, 1]);
      run(350); // Strike's cancel point is 450 ms in
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

    it.each(['held through it', 'pressed afresh', 'held again after a release-all'] as const)(
      'with attack %s, the key frees the player from Strike at its cancel point, 450 ms in',
      async (how) => {
        const key = (type: 'keydown' | 'keyup', code: string, init: KeyboardEventInit = {}) =>
          window.dispatchEvent(new KeyboardEvent(type, { code, cancelable: true, ...init }));
        const { attacks, run } = await fighting();
        key('keydown', 'KeyX');
        run(650);
        press('Digit1'); // Strike cuts the warrior's attack at 750 ms, and its own cancel point is at 1200
        run(200);
        expect([uses().length, attacks()]).toEqual([1, 1]);
        if (how === 'pressed afresh') {
          key('keyup', 'KeyX');
          key('keydown', 'KeyX');
        } else if (how === 'held again after a release-all') {
          key('keyup', 'MetaLeft', { key: 'Meta' }); // Cmd lets every key go
          key('keydown', 'KeyX', { repeat: true });
        }
        run(200);
        expect(attacks()).toBe(1);
        run(200);
        expect(attacks()).toBe(2);
        key('keyup', 'KeyX');
      },
    );

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

    it('an attack draws its beats; Strike draws its own and flashes its colour', async () => {
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const { run } = await fighting();
      press('KeyX');
      run(FRAME_MS);
      const [swing, flash] = act.mock.calls[0]!;
      expect(flash).toBeUndefined();
      expect(swing.cancelAt - swing.start).toBe(750); // the warrior's 900 ms, its cancel point at 15 of 18 ticks
      run(900);
      press('Digit1');
      run(FRAME_MS);
      expect(act).toHaveBeenCalledTimes(2);
      expect(act.mock.calls[1]?.[1]).toBe(0xff6a3d); // Strike's physical colour
    });

    it('every attack ends any casting from the input after its press, Strike casts from the input after its own, and a refusal takes the casting back', async () => {
      const mark = vi.spyOn(Predictor.prototype, 'mark');
      const drop = vi.spyOn(Predictor.prototype, 'drop');
      const sent = (t: string) =>
        FakeWebSocket.latest()
          .frames()
          .filter((f) => f.t === t);
      const { run } = await fighting();
      press('KeyX');
      run(FRAME_MS);
      expect(mark).toHaveBeenCalledExactlyOnceWith(sent('attack')[0]?.seq, 0); // with no skill running, a no-op
      run(900); // the warrior's attack ends
      press('Digit1');
      run(FRAME_MS);
      const strike = uses()[0]!;
      expect(strike.seq).toBeGreaterThan(0);
      expect(mark).toHaveBeenLastCalledWith(strike.seq, 90); // 30 ticks of 3 steps
      run(1250); // to Strike's cancel point
      press('KeyX');
      run(FRAME_MS);
      expect(mark).toHaveBeenLastCalledWith(sent('attack').at(-1)?.seq, 0);
      run(3200); // past Strike's 4.5 s cooldown
      press('Digit1');
      run(FRAME_MS);
      const refused = uses()[1]!;
      FakeWebSocket.latest().push(
        'response',
        { ok: false, code: ERR.NOT_ENOUGH_ENERGY },
        { req: refused.req },
      );
      await vi.advanceTimersByTimeAsync(0); // the refusal settles
      expect(drop).toHaveBeenCalledExactlyOnceWith(mark.mock.results.at(-1)?.value);
    });

    it('Haste paces the prediction faster from the input after its hit and back from the one after its end, at the sheet it then has, and a refusal takes both back', async () => {
      const pace = vi.spyOn(Predictor.prototype, 'pace');
      const dropPace = vi.spyOn(Predictor.prototype, 'dropPace');
      const world = await playing('forest');
      leave = world.leave;
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok-1',
          mapId: 'forest',
          character: {
            id: ME,
            name: 'Ayla',
            class: 'thief',
            level: 10,
            hp: 150,
            maxHp: 204,
            sheet: { derived: { moveSpeed: 0.01 }, skills: { unspent: 0, invested: { haste: 1 } } },
          },
        }),
      );
      energy.getState().welcome(10);
      skillBar.getState().load(['haste']);
      press('Digit1');
      const haste = uses()[0]!;
      const paced = () => pace.mock.calls.map(([after, p]) => [after, p()]);
      expect(paced()).toEqual([
        [haste.seq, faster(params, 1.095)], // its hit lands as it starts, 8.5% on the sheet's 1%
        [haste.seq + 400, faster(params, 1.01)], // 20 s on
      ]);
      // Boots put on during Haste: its end gives the new sheet's speed without Haste's share.
      character.getState().applyStatsChanged(
        create(StatsChangedSchema, {
          hp: 150,
          maxHp: 204,
          sheet: {
            derived: { moveSpeed: 0.135 },
            derivedSkills: { moveSpeed: 0.085 },
            skills: { unspent: 0, invested: { haste: 1 } },
          },
        }),
      );
      expect(paced()[1]).toEqual([haste.seq + 400, faster(params, 1.05)]);
      FakeWebSocket.latest().push(
        'response',
        { ok: false, code: ERR.NOT_ENOUGH_ENERGY },
        { req: haste.req },
      );
      await vi.advanceTimersByTimeAsync(0);
      const paces = pace.mock.results.map((r) => r.value as Pace);
      expect(dropPace.mock.calls).toEqual(paces.toReversed().map((m) => [m])); // the last applied first
    });

    it("another player's skill draws on them: its beats from its arrival, and its flash in its colour", async () => {
      const world = await playing('forest');
      leave = world.leave;
      const act = vi.spyOn(PlayerView.prototype, 'act');
      robinBeside(world.frames);
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
      expect(beats.cancelAt - beats.start).toBe(450); // Spark's 30 ticks, the cancel point at 9
      expect([beats.slows, beats.end - beats.start]).toEqual([true, 1500]); // drawn slowed to its end, unless an attack of theirs comes first
    });

    it.each([
      { cls: 'warrior', drawn: 'a slash through its box' },
      { cls: 'bowman', drawn: 'its arrow' },
    ])(
      "another player's basic attack draws on them over its length, with $drawn; the player's own comes back already drawn",
      async ({ cls }) => {
        const world = await playing('forest');
        leave = world.leave;
        const act = vi.spyOn(PlayerView.prototype, 'act');
        const look = vi.spyOn(World.prototype as unknown as Drawing, 'attackLook');
        const effect = vi.spyOn(World.prototype as unknown as Drawing, 'effect');
        const cut = vi.spyOn(World.prototype as unknown as Drawing, 'cutShort');
        const at = robinBeside(world.frames, cls);
        FakeWebSocket.latest().push('attackUsed', { attacker: ME, facing: 1, lengthTicks: 18 });
        FakeWebSocket.latest().push('attackUsed', {
          attacker: 'c-robin',
          facing: -1,
          lengthTicks: 14,
        });
        world.frames(1);
        expect(act).toHaveBeenCalledOnce();
        const [beats, flash] = act.mock.calls[0]!;
        expect(flash).toBeUndefined();
        expect(beats.cancelAt - beats.start).toBe(550); // 14 ticks, the cancel point at ⌊14 × 0.85⌋ = 11
        expect(beats.slows).toBe(false); // in place of any skill's slow still drawn
        const o = { x: at.x, y: at.y, facing: -1, beats };
        expect(look).toHaveBeenCalledExactlyOnceWith('c-robin', cls, o);
        if (cls === 'warrior') {
          expect(effect).toHaveBeenCalledExactlyOnceWith('c-robin', 'attack.warrior', o);
        } else {
          expect(effect).not.toHaveBeenCalled(); // the arrow flies on whatever Robin does next
          expect(cut).toHaveBeenCalledWith('c-robin'); // an effect of Robin's still showing goes
        }
      },
    );

    it("a mob's attack plays its look once from its windup and ends with it; a blow breaking it off ends it, and a spit's lasts while its ball flies", async () => {
      const frame = vi.spyOn(World.prototype as unknown as Drawing, 'playMobAttacks');
      const world = await playing('forest');
      leave = world.leave;
      mobsBeside(world.frames, 2);
      const me = snapshotOn('forest').players![0]!;
      const x = me.x ?? 0;
      const at = { y: me.y ?? 0, facing: -1, hp: 34, maxHp: 34 };
      const slime = { id: 1, type: 'green_slime', x: x + 60, ...at };
      const bat = { id: 2, type: 'cave_bat', x: x + 120, ...at };
      const ws = FakeWebSocket.latest();
      let tick = 9;
      const run = (ticks: number, flinch = false) => {
        for (let i = 0; i < ticks; i++, tick++) {
          const s = flinch ? { ...slime, pose: MobPose.FLINCH } : slime;
          ws.push('delta', { mobs: [s, bat] }, { tick });
          world.frames(1);
        }
      };
      const looks = () => {
        const scene = frame.mock.contexts.at(-1) as {
          mobLooks: Map<number, { swing: { attack: { name: string } } }>;
        };
        return [...scene.mobLooks].map(([id, l]) => `${id} ${l.swing.attack.name}`);
      };
      ws.push('mobAttacked', { mob: 1, attack: 0, facing: -1 }, { tick });
      ws.push('mobAttacked', { mob: 2, attack: 0, facing: -1 }, { tick });
      run(6); // its windup under way, 600 ms long
      expect(looks()).toEqual(['1 hop', '2 spit']);
      const first = frame.mock.contexts.at(-1) as { mobLooks: Map<number, unknown> };
      const hop = first.mobLooks.get(1);
      run(4, true); // the slime flinches before its act, drawn a moment later
      expect(looks()).toEqual(['2 spit']);
      expect(hop).toBeDefined();
      run(12); // the spit thrown at 600 ms and its 250 ms impact over, its ball still flying its 800 ms
      const poses = frame.mock.calls.at(-1)?.[0] as Map<number, unknown>;
      expect([poses.has(2), looks()]).toEqual([false, ['2 spit']]);
      run(8);
      expect(looks()).toEqual([]);
    });

    it('a player gone from view takes their effect and their latest damage type with them', async () => {
      const effect = vi.spyOn(World.prototype as unknown as Drawing, 'effect');
      const world = await playing('forest');
      leave = world.leave;
      robinBeside(world.frames);
      const ws = FakeWebSocket.latest();
      ws.push('attackUsed', { attacker: 'c-robin', facing: -1, lengthTicks: 14 });
      const scene = effect.mock.contexts.at(-1) as {
        effects: Map<string, unknown>;
        lastDamage: Map<string, unknown>;
      };
      const kept = () => [scene.effects.has('c-robin'), scene.lastDamage.has('p:c-robin')];
      expect(kept()).toEqual([true, true]);
      for (let tick = 11; tick <= 20; tick++) {
        ws.push('delta', tick === 11 ? { removedPlayers: ['c-robin'] } : {}, { tick });
        world.frames(1);
      }
      expect(kept()).toEqual([false, false]);
    });

    it("the player's own hit holds their pose and each mob it struck; a MISS, or another's hit, holds nothing", async () => {
      const world = await playing('forest');
      leave = world.leave;
      mobsBeside(world.frames, 2);
      const stop = vi.spyOn(PlayerView.prototype, 'hitStop');
      const hold = vi.spyOn(MobView.prototype, 'hold');
      press('KeyX');
      world.frames(1);
      const ws = FakeWebSocket.latest();
      ws.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 0, miss: true });
      ws.push('damage', { source: 'p:c-robin', target: 'm:1', amount: 7 });
      ws.push('damage', { source: 'm:2', target: `p:${ME}`, amount: 3 });
      expect(stop).not.toHaveBeenCalled();
      ws.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 });
      ws.push('damage', { source: `p:${ME}`, target: 'm:2', amount: 7 }); // the same swing's second foe
      expect(stop).toHaveBeenCalledTimes(2);
      const h = stop.mock.results[0]?.value as { from: number; until: number };
      expect(h.until - h.from).toBe(50);
      expect(stop.mock.results[1]?.value).toBe(h); // one hold for both
      expect(hold.mock.calls).toEqual([[h.until], [h.until]]);
    });

    it("the player's own arrow shows its hit as it reaches the mob: its number, spark and hit-stop wait for the drawn shot", async () => {
      const world = await playing('forest', 'bowman');
      leave = world.leave;
      const x = (snapshotOn('forest').players![0]!.x ?? 0) + 280; // 200 ms of flight at a shot's 1.4 px/ms
      slimeAt(world.frames, 2, x);
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const stop = vi.spyOn(PlayerView.prototype, 'hitStop');
      const spawn = vi.spyOn(DamageNumbers.prototype, 'spawn');
      press('KeyX');
      world.frames(1);
      const beats = act.mock.calls[0]![0];
      FakeWebSocket.latest().push('damage', { source: `p:${ME}`, target: 'm:9', amount: 7 }); // the server's hit, a round trip on
      world.frames(2);
      expect([stop.mock.calls.length, spawn.mock.calls.length]).toEqual([0, 0]);
      world.frames(3);
      expect(stop.mock.calls).toEqual([[expect.any(Number), beats]]);
      const [shownX, , , , shownAt] = spawn.mock.calls[0]!;
      expect(shownX).toBe(x);
      expect(shownAt).toBe(stop.mock.calls[0]![0]); // stacking measures from when it shows
      expect(shownAt - beats.hit).toBeGreaterThanOrEqual(200);
      expect(shownAt - beats.hit).toBeLessThan(200 + FRAME_MS);
    });

    it('a full-range Power Shot holds the pose as it lands, late in the action, and the hold ends by the cancel point', async () => {
      const world = await playing('forest', 'bowman');
      leave = world.leave;
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok-1',
          mapId: 'forest',
          character: {
            id: ME,
            name: 'Ayla',
            class: 'bowman',
            level: 10,
            hp: 150,
            maxHp: 204,
            sheet: { skills: { unspent: 0, invested: { power_shot: 1 } } },
          },
        }),
      );
      energy.getState().welcome(10);
      skillBar.getState().load(['power_shot']);
      slimeAt(world.frames, 2, (snapshotOn('forest').players![0]!.x ?? 0) + 460); // its full range: 329 ms of flight, its cancel point 450 ms on
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const stop = vi.spyOn(PlayerView.prototype, 'hitStop');
      press('Digit1');
      world.frames(1);
      const beats = act.mock.calls[0]![0];
      FakeWebSocket.latest().push('damage', { source: `p:${ME}`, target: 'm:9', amount: 30 });
      world.frames(Math.ceil((beats.cancelAt - beats.hit) / FRAME_MS));
      const h = stop.mock.results[0]?.value as Hold;
      expect(h.from - beats.hit).toBeGreaterThanOrEqual(329);
      expect(h).toEqual({
        from: h.from,
        until: Math.min(h.from + 50, beats.cancelAt),
        cancelAt: beats.cancelAt,
      });
      expect(heldTime(h, beats.cancelAt)).toBe(beats.cancelAt); // ready on the cancel point, held or not
    });

    it("another player's arrow, its AttackUsed and Damage in one frame as the server sends them, shows its hit as the drawn arrow reaches the mob", async () => {
      const world = await playing('forest');
      leave = world.leave;
      const robin = robinBeside(world.frames, 'bowman');
      slimeAt(world.frames, 11, robin.x + 280); // 200 ms of flight from Robin
      const act = vi.spyOn(PlayerView.prototype, 'act');
      const spawn = vi.spyOn(DamageNumbers.prototype, 'spawn');
      const ws = FakeWebSocket.latest();
      ws.push('attackUsed', { attacker: 'c-robin', facing: 1, lengthTicks: 21 }, { tick: 18 });
      ws.push('damage', { source: 'p:c-robin', target: 'm:9', amount: 7 }, { tick: 18 });
      world.frames(2);
      expect(spawn).not.toHaveBeenCalled();
      world.frames(3);
      const beats = act.mock.calls[0]![0];
      const [shownX, , , , shownAt] = spawn.mock.calls[0]!;
      expect(shownX).toBe(robin.x + 280);
      expect(shownAt - beats.hit).toBeGreaterThanOrEqual(200);
      expect(shownAt - beats.hit).toBeLessThan(200 + FRAME_MS);
    });

    it("another player's arrow shows its hit as it reaches the mob, where the mob was last drawn if it's gone by then", async () => {
      const world = await playing('forest');
      leave = world.leave;
      const robin = robinBeside(world.frames, 'bowman');
      slimeAt(world.frames, 11, robin.x + 280); // 200 ms of flight from Robin
      const spawn = vi.spyOn(DamageNumbers.prototype, 'spawn');
      const ws = FakeWebSocket.latest();
      ws.push('attackUsed', { attacker: 'c-robin', facing: 1, lengthTicks: 21 }, { tick: 18 });
      ws.push('damage', { source: 'p:c-robin', target: 'm:9', amount: 34 }, { tick: 18 });
      world.frames(1);
      expect(spawn).not.toHaveBeenCalled();
      for (let tick = 19; tick < 26; tick++) {
        ws.push('delta', tick === 19 ? { removedMobs: [9] } : {}, { tick }); // killed: gone before the arrow gets there
        world.frames(1);
      }
      expect(spawn.mock.calls.map(([x, , text]) => [x, text])).toEqual([[robin.x + 280, '34']]);
    });

    it("a hit sparks in its source's damage type: a player's latest attack or skill, a mob type's own, else physical; its number shows over its target", async () => {
      const world = await playing('forest');
      leave = world.leave;
      robinBeside(world.frames, 'magician');
      mobsBeside(world.frames, 11);
      const from = vi.spyOn(World.prototype as unknown as Sparks, 'damageFrom');
      const spawn = vi.spyOn(DamageNumbers.prototype, 'spawn');
      const ws = FakeWebSocket.latest();
      const hit = (source: string, target = 'm:1') =>
        ws.push('damage', { source, target, amount: 4 });
      hit('p:c-robin'); // nothing of Robin's seen yet
      ws.push('attackUsed', { attacker: 'c-robin', facing: -1, lengthTicks: 14 });
      hit('p:c-robin'); // a Magician's orb
      ws.push('skillUsed', { caster: 'c-robin', skill: 'strike', stage: 0, facing: -1 });
      hit('p:c-robin');
      hit('m:2', `p:${ME}`); // a cave bat's bite
      hit('m:1', `p:${ME}`);
      expect(from.mock.results.map((r) => r.value as string)).toEqual([
        'physical',
        'magic',
        'physical',
        'magic',
        'physical',
      ]);
      expect(spawn.mock.calls[0]?.slice(2)).toEqual(['4', 'hit', expect.any(Number)]);
    });

    it("a damaging skill's look goes to the foes it hits, each once, misses aside", async () => {
      const effect = vi.spyOn(World.prototype as unknown as Drawing, 'effect');
      const world = await playing('forest');
      leave = world.leave;
      learned();
      mobsBeside(world.frames, 2);
      press('Digit1');
      expect(effect.mock.calls[0]?.slice(0, 2)).toEqual([ME, 'skill.strike']);
      const ws = FakeWebSocket.latest();
      for (const [target, miss] of [
        ['m:2', false],
        ['m:1', false],
        ['m:2', false],
        ['m:1', true],
      ] as const) {
        ws.push('damage', { source: `p:${ME}`, target, amount: miss ? 0 : 5, miss });
      }
      expect(targetsOf(effect, ME)).toEqual(['m:2', 'm:1']);
    });

    it("another's skill gathers only the hits before its cancel point; a buff gathers none, and another's heal goes to them alone", async () => {
      const effect = vi.spyOn(World.prototype as unknown as Drawing, 'effect');
      const world = await playing('forest');
      leave = world.leave;
      robinBeside(world.frames);
      mobsBeside(world.frames, 11);
      const ws = FakeWebSocket.latest();
      const use = (skill: string) =>
        ws.push('skillUsed', { caster: 'c-robin', skill, stage: 0, facing: 1 });
      use('strike');
      world.frames(10); // past Strike's cancel point, 450 ms in
      ws.push('damage', { source: 'p:c-robin', target: 'm:1', amount: 5 });
      expect(targetsOf(effect, 'c-robin')).toEqual([]);
      use('haste'); // its aura follows Robin instead
      ws.push('damage', { source: 'p:c-robin', target: 'm:1', amount: 5 });
      expect(targetsOf(effect, 'c-robin')).toEqual([]);
      use('heal');
      expect(targetsOf(effect, 'c-robin')).toEqual(['p:c-robin']);
    });

    it.each([
      { hp: 150, want: [`p:${ME}`, 'p:c-robin'], who: 'themselves, hurt,' },
      { hp: 204, want: ['p:c-robin'], who: 'not themselves at full HP, but' },
    ])(
      "the player's own heal goes to whom the world will find: $who the hurt members drawn within its reach",
      async ({ hp, want }) => {
        const effect = vi.spyOn(World.prototype as unknown as Drawing, 'effect');
        const world = await playing('forest');
        leave = world.leave;
        robinBeside(world.frames); // 100 px away, within the heal's 150
        const member = (characterId: string, memberHp: number) => ({
          characterId,
          name: characterId,
          level: 10,
          hp: memberHp,
          maxHp: 100,
          mapId: 'forest',
          online: true,
          class: 'warrior',
        });
        character.getState().applyWelcome(
          create(WelcomeSchema, {
            token: 'tok-1',
            mapId: 'forest',
            character: {
              id: ME,
              name: 'Ayla',
              class: 'magician',
              level: 30,
              hp,
              maxHp: 204,
              sheet: { skills: { unspent: 0, invested: { heal: 1 } } },
            },
          }),
        );
        energy.getState().welcome(10);
        skillBar.getState().load(['heal']);
        const members = [member(ME, hp), member('c-robin', 40), member('c-far', 40)]; // c-far isn't drawn here
        party
          .getState()
          .applyPartyState(create(PartyStateSchema, { party: { leader: ME, members } }));
        press('Digit1');
        expect(effect.mock.calls[0]?.slice(1, 2)).toEqual(['skill.heal']);
        expect(effect.mock.calls[0]?.[3]).toEqual(want);
      },
    );

    it("an afterimage shows each cancel as it happens, never ahead of it, and not for an attack after another's end", async () => {
      const afterimage = vi.spyOn(World.prototype as unknown as Drawing, 'afterimage');
      const { attacks, run } = await fighting();
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
      run(950); // the warrior's attack ends at 900 and the held key swings again
      expect([attacks(), afterimage.mock.calls.length]).toEqual([2, 0]);
      run(600); // 650 into the second
      press('Digit1'); // waits for its cancel point, 100 ms on
      run(50);
      expect(afterimage).not.toHaveBeenCalled();
      run(100);
      expect([uses().length, afterimage.mock.calls.length]).toEqual([1, 1]); // Strike cut the attack short
      run(450); // Strike's cancel point: the held attack cuts it short
      expect([attacks(), afterimage.mock.calls.length]).toEqual([3, 2]);
      window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyX', cancelable: true }));
    });

    it('a hit-stop holds the drawing, not the action: Strike still frees the held attack at its cancel point', async () => {
      const stop = vi.spyOn(PlayerView.prototype, 'hitStop');
      const { attacks, run } = await fighting();
      window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyX', cancelable: true }));
      run(650);
      press('Digit1'); // Strike cuts the warrior's attack at 750 ms, and its own cancel point is at 1200
      run(200);
      FakeWebSocket.latest().push('damage', { source: `p:${ME}`, target: 'm:1', amount: 9 });
      expect(stop.mock.results.at(-1)?.value).not.toBeNull();
      run(200);
      expect([uses().length, attacks()]).toEqual([1, 1]);
      run(200);
      expect(attacks()).toBe(2);
      window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyX', cancelable: true }));
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
    expect(attacks()).toBe(2); // the Thief's 650 ms, where the Warrior it entered as waits 900
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
