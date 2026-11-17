import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import type { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Node } from '@babylonjs/core/node';
import type { Scene } from '@babylonjs/core/scene';
import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { classes } from '@/gamedata/classes';
import { maps } from '@/gamedata/maps';
import { mobs as mobData } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';
import { ticksIn, tickTiming } from '@/lib/fixed-step';
import { GameSocket } from '@/net/socket';
import { clearToken, setToken } from '@/net/token';
import { actionPoints } from '@/progression/actions';
import { mobById } from '@/progression/mobs';
import { npcsOn } from '@/progression/quests';
import { skillById } from '@/progression/skills';
import { statusColor } from '@/progression/statuses';
import { ERR } from '@/protocol/errors';
import {
  FieldBossGoneSchema,
  FieldBossSchema,
  MobPose,
  PartyStateSchema,
  PartyViewSchema,
  StatusViewSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { dash, step } from '@/sim/step';
import type { State } from '@/sim/step';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { mapView } from '@/state/map-view';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { skillBar } from '@/state/skill-bar';
import { target } from '@/state/target';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { mapJson, servePublic } from '@/testing/public-on-disk';
import { resetStores } from '@/testing/reset-stores';

import { Fx } from '../effects/fx';
import type { Body as Unit } from '../effects/types';
import { attackIn, attackTiming, skillTiming } from '../systems/actions';
import { Arrival } from '../systems/arrival';
import { MobAttacks } from '../systems/mob-attacks';

import { beatsOf } from './beats';
import { BLEND_MS } from './blend';
import { Body, FADED } from './body';
import { Figures } from './figures';
import { classLook, mobLook } from './looks';
import { loadMap } from './map';
import { World } from './world';

const ME = 'c-ayla';
const RATE = 20;
const FRAME_MS = 50;
const MODELS = path.resolve(process.cwd(), 'public/models');
const WAIT = { timeout: 3000, interval: 5 }; // polled every 5 ms, not 50; 3 s for a build on a CI runner under coverage

vi.mock('./kit-models', () => ({ kitModels: {} })); // every key a stand-in: place.test draws town and forest with the real kit models

/** Hunting maps with no NPCs, served flat and bare for the tests no map's own layout bears on: FLAT, and the two a map change goes to. */
const [FLAT, FLAT2, FLAT3] = maps
  .filter((m) => m.kind === 'hunting' && npcsOn(m.id).length === 0)
  .map((m) => m.id) as [string, string, string];

/** Map id flat and bare, 72 by 40 m. */
function bare(id: string) {
  return {
    id,
    ground: 'grass',
    sky: 'day',
    geometry: {
      size_x: 72,
      size_z: 40,
      terrain: { cell: 8, nx: 10, nz: 6, heights: Array<number>(60).fill(0) },
      boxes: [],
      cylinders: [],
    },
    box_models: [],
    cylinder_models: [],
    props: [],
    portals: [],
  };
}

/** Map id's own size, portals and NPCs, on flat ground with nothing built on it: for the tests of its NPCs and portals, not its houses. */
function slim(id: string) {
  const m = mapJson(id) as ReturnType<typeof bare>;
  const t = m.geometry.terrain;
  return {
    ...bare(id),
    ground: m.ground,
    sky: m.sky,
    geometry: {
      ...bare(id).geometry,
      size_x: m.geometry.size_x,
      size_z: m.geometry.size_z,
      terrain: { ...t, heights: t.heights.map(() => 0) },
    },
    portals: m.portals,
  };
}

/** The maps served in place of the real ones, made once; the rest come from disk. */
const SERVED = {
  [FLAT]: bare(FLAT),
  [FLAT2]: bare(FLAT2),
  [FLAT3]: bare(FLAT3),
  town: slim('town'),
};

type PlayerWire = NonNullable<ServerInit<'snapshot'>['players']>[number];
type MobWire = NonNullable<ServerInit<'snapshot'>['mobs']>[number];

/** The local player in state s, as a frame carries them. */
function wireOf(s: State): PlayerWire {
  const { on_ground: onGround, ...rest } = s;
  return { id: ME, name: 'Ayla', class: 'warrior', ...rest, onGround, hp: 50, maxHp: 50 };
}

/** The player standing at (x, z) on flat ground, facing east. */
function standing(x: number, z: number, id = ME, cls = 'warrior'): PlayerWire {
  return {
    id,
    name: id === ME ? 'Ayla' : 'Robin',
    class: cls,
    x,
    y: 0,
    z,
    onGround: true,
    hx: 1,
    hz: 0,
    hp: 50,
    maxHp: 50,
  };
}

interface Playing {
  readonly world: World;
  readonly parent: HTMLDivElement;
  readonly socket: GameSocket;
  readonly ws: FakeWebSocket; // its own, not the latest: another test's socket may reconnect meanwhile
  scene(): Scene | undefined;
  frames(n: number): void;
  live(n: number): void;
  inputs(): number;
  sent(t: string): Record<string, unknown>[];
  root(file: string): TransformNode | undefined;
  label(key: string): string | null | undefined;
  push: FakeWebSocket['push'];
}

const open: { world: World; socket: GameSocket; engine: NullEngine; left?: boolean }[] = [];
/** A model file served as junk, as a broken download would be. */
let broken = '';

/** A world on a real socket, entered on mapId as cls and placed by its first Snapshot at (x, z). */
async function playing(mapId: string, x: number, z: number, cls = 'warrior'): Promise<Playing> {
  const socket = new GameSocket({ url: 'ws://game.test/ws' });
  setToken('tok-login');
  const welcomed = socket.connect(ME);
  const ws = FakeWebSocket.latest();
  ws.open();
  ws.push('welcome', { token: 'tok-1', mapId, tickRateHz: RATE });
  await welcomed;
  const engine = new NullEngine();
  engine.getCaps().instancedArrays = true; // the scatter's thin instances
  const parent = document.createElement('div');
  document.body.appendChild(parent);
  let t = 0;
  let tick = 1;
  const arriving = new Arrival(socket, mapId, RATE);
  ws.push('snapshot', { players: [standing(x, z, ME, cls)] }, { tick: 1 }); // held until the map is built
  servePublic(SERVED);
  const world = World.open(
    {
      engine,
      socket,
      parent,
      player: { characterId: ME, cls, name: 'Ayla' },
      models: (file) =>
        file === broken
          ? new Uint8Array([1, 2, 3])
          : new Uint8Array(readFileSync(path.join(MODELS, file))),
      now: () => t,
    },
    arriving,
  );
  open.push({ world, socket, engine });
  const p: Playing = {
    world,
    parent,
    socket,
    frames: (n) => {
      for (let i = 0; i < n; i++) {
        t += FRAME_MS;
        if (vi.isFakeTimers()) {
          vi.advanceTimersByTime(FRAME_MS);
        }
        world.frame(FRAME_MS);
      }
    },
    live: (n) => {
      for (let i = 0; i < n; i++) {
        p.push('delta', {}, { tick: ++tick });
        p.frames(1);
      }
    },
    inputs: () => p.sent('input').length,
    ws,
    sent: (kind) =>
      ws
        .frames()
        .filter((f) => f.t === kind)
        .map((f) => f.d as unknown as Record<string, unknown>),
    scene: () => engine.scenes.at(-1),
    root: (file) =>
      engine.scenes.at(-1)?.transformNodes.find((n) => n.name === file && n.isEnabled()),
    label: (key) => parent.querySelector(`[data-label="${key}"]`)?.textContent,
    push: (...args) => ws.push(...args),
  };
  await vi.waitFor(() => expect(p.root(classLook(cls).file)).toBeDefined(), WAIT);
  p.frames(1);
  return p;
}

/** Pushes a MapChange on a fake setTimeout and runs the veil's fade it starts at once; the build after it keeps real timers. */
function changeMap(p: Playing, d: ServerInit<'mapChange'>): void {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  p.push('mapChange', d, { tick: 2 });
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
}

/** Takes p's world down as leaving the game does, its socket closed with it, so neither outlives the test; its engine goes after. */
function leave(p: Playing): void {
  p.world.dispose();
  p.socket.close();
  const o = open.find((e) => e.world === p.world);
  if (o) {
    o.left = true;
  }
}

function key(code: string, down: boolean): void {
  window.dispatchEvent(new KeyboardEvent(down ? 'keydown' : 'keyup', { code }));
}

/** The weight of the clip named name on the body under root. */
function weight(root: TransformNode | undefined, name: string): number {
  const g = root
    ?.getScene()
    .animationGroups.find(
      (a) =>
        a.name === name &&
        a.targetedAnimations.some((t) => t.target instanceof Node && t.target.isDescendantOf(root)),
    );
  return g?.weight ?? NaN;
}

const mob = (id: number, type: string, x: number, pose = MobPose.UNSPECIFIED): MobWire => ({
  id,
  type,
  x,
  y: 0,
  z: 20,
  yaw: -Math.PI / 2,
  hp: 30,
  maxHp: 34,
  pose,
});

/** Sends mobs for ticks from on, drawing a frame after each. */
function frames(p: Playing, from: number, to: number, mobs: MobWire[]): void {
  for (let tick = from; tick <= to; tick++) {
    p.push('delta', { mobs }, { tick });
    p.frames(1);
  }
}

/** The damage numbers shown, as [kind, text]. */
function numbers(p: Playing): [string, string][] {
  return [...p.parent.querySelectorAll<HTMLElement>('[data-number]')]
    .filter((e) => e.style.display !== 'none')
    .map((e) => [e.dataset.number ?? '', e.textContent ?? '']);
}

describe('World', () => {
  beforeEach(() => {
    FakeWebSocket.reset();
    vi.stubGlobal('WebSocket', FakeWebSocket);
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = vi.fn();
        disconnect = vi.fn();
      },
    ); // jsdom has none, and lays nothing out to watch
    resetStores();
  });

  afterEach(() => {
    for (const o of open.splice(0)) {
      if (!o.left) {
        o.world.dispose();
      }
      o.socket.close();
      o.engine.dispose();
    }
    document.body.replaceChildren();
    broken = '';
    vi.unstubAllGlobals();
    vi.useRealTimers();
    clearToken();
  });

  it('draws the map and places the player where its Snapshot says, sending an Input each tick', async () => {
    const p = await playing('forest', 4, 20);
    const me = p.root(classLook('warrior').file)!;
    expect([me.position.x, me.position.y, me.position.z]).toEqual([4, 0, 20]);
    expect(p.scene()?.transformNodes.some((n) => n.name === 'place forest')).toBe(true);
    const before = p.inputs();
    p.frames(10);
    expect(p.inputs() - before).toBe(10);
    expect(p.label('me')).toBe('Ayla');
  });

  it('walks the player east till the tree in its way stops them, as the server’s sim would', async () => {
    const p = await playing('forest', 4, 20);
    key('ArrowRight', true);
    p.live(60);
    key('ArrowRight', false);
    p.live(2);
    const me = p.root(classLook('warrior').file)!;
    expect(me.position.x).toBeCloseTo(16 - 0.34 - movement.radius, 3); // the oak at 16, 20
    expect(me.position.z).toBeCloseTo(20, 6);
    expect(p.sent('input').some((d) => d.right === true)).toBe(true);
  });

  it('eases the camera after the player, time-based, and holds it inside the map', async () => {
    const p = await playing(FLAT, 4, 20);
    const camera = p.scene()!.activeCamera!;
    p.live(40);
    const held = camera.position.x;
    expect(held).toBeGreaterThan(4); // the map's west edge holds the view
    key('ArrowRight', true);
    p.live(30);
    key('ArrowRight', false);
    expect(camera.position.x).toBeGreaterThan(held);
  });

  it('draws the NPCs with their names, and the portals with the maps they go to', async () => {
    const p = await playing('town', 14, 16, 'thief');
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT); // the warrior trainer
    expect(p.label('npc:town_guide')).toBe('Town Guide');
    expect(p.label('npc:warrior_trainer')).toBe('Warrior Trainer');
    expect(p.label('portal:to_forest')).toBe('To Forest');
  });

  it('talks to the NPC in reach with the talk key, and in a portal enters it instead', async () => {
    const near = await playing('town', 16, 14.5, 'thief'); // 1.5 m from the guide
    key('KeyZ', true);
    key('KeyZ', false);
    expect(near.sent('talk')).toEqual([expect.objectContaining({ npcId: 'town_guide' })]);
    leave(near);

    const inPortal = await playing('town', 46, 16, 'thief');
    key('KeyZ', true);
    key('KeyZ', false);
    expect([inPortal.sent('enterPortal').length, inPortal.sent('talk').length]).toEqual([1, 0]);
  });

  it('draws other players behind the newest frame, lists them nearby, and takes them away when removed', async () => {
    const p = await playing(FLAT, 4, 20);
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', tick === 2 ? { players: [standing(8, 20, 'c-robin', 'thief')] } : {}, {
        tick,
      });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
    p.frames(1);
    expect(p.root(classLook('thief').file)!.position.x).toBeCloseTo(8);
    expect(nearby.getState().players.map((n) => n.id)).toEqual(['c-robin']);
    expect(p.label('p:c-robin')).toBe('Robin');
    for (let tick = 9; tick <= 14; tick++) {
      p.push('delta', tick === 9 ? { removedPlayers: ['c-robin'] } : {}, { tick });
      p.frames(1);
    }
    expect(p.root(classLook('thief').file)).toBeUndefined();
    expect(nearby.getState().players).toEqual([]);
  });

  describe('mobs', () => {
    it('draw each one’s model where the frames say', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 8), mob(2, 'wild_boar', 11)]);
      await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
      frames(p, 11, 12, [mob(1, 'green_slime', 8), mob(2, 'wild_boar', 11)]);
      expect(p.root('quaternius/GreenBlob.glb')!.position.x).toBeCloseTo(8);
      const boar = p.root('quaternius/Blob_Dog.glb')!;
      expect([boar.position.x, boar.rotation.y]).toEqual([
        expect.closeTo(11),
        expect.closeTo(-Math.PI / 2, 1),
      ]);
    });

    it('take a pose as the frames reach it, and a killed one lies a second, then goes', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 8)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      frames(p, 11, 14, [mob(1, 'green_slime', 8, MobPose.DOWN)]);
      p.frames(4);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Death')).toBe(1);
      p.push('delta', { removedMobs: [1] }, { tick: 15 });
      p.frames(6);
      expect(p.root('quaternius/GreenBlob.glb')).toBeDefined();
      p.frames(20);
      expect(p.root('quaternius/GreenBlob.glb')).toBeUndefined();
    });

    it('show an HP bar over each one’s head, an elite’s in gold, none over a boss or a killed one', async () => {
      const p = await playing(FLAT, 4, 20);
      const bar = (id: number) => p.parent.querySelector<HTMLElement>(`[data-bar="${id}"]`);
      const all = [mob(1, 'green_slime', 6), mob(2, 'sun_slime', 8), mob(3, 'king_slime', 10)];
      frames(p, 2, 8, all);
      expect(bar(1)?.style.display).toBe('');
      expect((bar(1)?.firstElementChild as HTMLElement).style.width).toBe(`${(30 / 34) * 100}%`);
      expect([bar(1)?.style.outline, bar(2)?.style.outline]).toEqual(['', '2px solid #f2c14e']);
      expect(bar(3)).toBeNull(); // the boss bar at the top shows its HP
      frames(p, 9, 12, [{ ...mob(1, 'green_slime', 6, MobPose.DOWN), hp: 0 }, ...all.slice(1)]);
      expect(bar(1)).toBeNull();
      p.push('delta', { removedMobs: [2] }, { tick: 13 });
      p.frames(4);
      expect(bar(2)).toBeNull();
    });

    it('play an attack as the drawn frames reach its windup', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      const telegraphs = () =>
        p.scene()!.meshes.filter((m) => m.name === 'patch' && m.visibility > 0).length;
      frames(p, 11, 11, [mob(1, 'green_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(0); // drawn 100 ms behind, it hasn't begun
      expect(telegraphs()).toBe(0);
      frames(p, 12, 16, [mob(1, 'green_slime', 6)]);
      expect(telegraphs()).toBe(2); // its landing's area, and what has filled of it
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(0); // its bite waits to strike on the landing
      frames(p, 17, 26, [mob(1, 'green_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(1);
      frames(p, 27, 40, [mob(1, 'green_slime', 6)]);
      expect(p.scene()!.meshes.filter((m) => m.name === 'patch')).toHaveLength(0);
    });

    // The world moves the player through a whole tick before its mobs: a player sidestepping a charge was judged part way through, still in its lane, and the drawn charge stopped on them while the world's ran on.
    it('judge a charge or a spit against the player where the tick being run leaves them, as the world does', async () => {
      const p = await playing(FLAT, 4, 20);
      key('ArrowRight', true);
      p.live(4);
      const judged = vi.spyOn(MobAttacks.prototype, 'frame');
      for (let i = 0; i < 6; i++) {
        p.world.frame(1000 / 60); // one of a tick's three steps a frame
      }
      key('ArrowRight', false);
      const xs = judged.mock.calls.map((c) => c[3]!.x);
      judged.mockRestore();
      expect(xs).toHaveLength(6);
      expect([xs[1] === xs[0], xs[2] === xs[0], xs[4] === xs[3], xs[5] === xs[3]]).toEqual([
        true,
        true,
        true,
        true,
      ]); // still through a tick's steps, as its input leaves them
      expect(xs[3]! - xs[0]!).toBeGreaterThan(0.1); // a whole tick's walk on
    });

    it('bring a charge’s bite to the world’s hit when it lands on a player its run wasn’t judged to meet', async () => {
      const snail = mobLook('mossback_snail');
      const clip = snail.clips.attack!;
      const p = await playing(FLAT, 4, 22); // 2 m off the lane as its run starts
      frames(p, 2, 10, [mob(1, 'mossback_snail', 8)]);
      await vi.waitFor(() => expect(p.root(snail.file)).toBeDefined(), WAIT);
      const root = p.root(snail.file)!;
      const g = root
        .getScene()
        .animationGroups.find(
          (a) =>
            a.name === clip.clip &&
            a.targetedAnimations.some(
              (t) => t.target instanceof Node && t.target.isDescendantOf(root),
            ),
        )!;
      const share = () => (g.getCurrentFrame() - g.from) / (g.to - g.from);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 2, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      const at = new Map<number, number>();
      for (let tick = 11; tick <= 34; tick++) {
        if (tick === 30) {
          p.push('damage', { source: 'm:1', target: `p:${ME}`, amount: 5 }, { tick: 29 }); // three ticks into its run
        }
        frames(p, tick, tick, [mob(1, 'mossback_snail', 8)]);
        at.set(tick, share());
      }
      // planned on its run's end, it would strike about tick 36; heard at 30, it plays on at 1.5 times its pace
      expect([at.get(30)! < clip.strike, at.get(32)! >= clip.strike]).toEqual([true, true]);
    });

    it('fill a windup on the player’s own clock, full as the world strikes them, ahead of the drawn frames, and hold it till the world has acted', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      const LEAD = 3; // ticks the player's inputs run ahead of the frames they hear
      const first = p.ws
        .frames()
        .filter((f) => f.t === 'input')
        .at(-1)!.seq; // runs at tick 11 + LEAD
      const hear = (tick: number) => {
        const ackSeq = first + tick - 11 - LEAD; // the input that ran at tick
        p.push(
          'delta',
          { mobs: [mob(1, 'green_slime', 6)], players: [standing(4, 20)], ackSeq },
          { tick },
        );
        p.frames(1); // predicts the next input, which runs at tick + LEAD + 1
      };
      hear(11);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 12 },
      );
      const windup = 600 / 50; // the hop's, in ticks
      const hit = 12 + windup;
      const patches = () => p.scene()!.meshes.filter((m) => m.name === 'patch' && m.visibility > 0);
      const reach = (m: AbstractMesh) => {
        const v = m.getVerticesData('position')!;
        let far = 0;
        for (let i = 0; i < v.length; i += 3) {
          far = Math.max(far, Math.hypot(v[i]! - 5, v[i + 2]! - 20)); // from where it lands
        }
        return far;
      };
      const filled = () => {
        const [zone, fill] = patches();
        return reach(fill!) / reach(zone!);
      };
      for (let tick = 12; tick + LEAD + 1 <= hit + 1; tick++) {
        hear(tick);
        const mine = tick + LEAD + 1; // the drawn player stands within the tick before it
        const [lo, hi] = [(mine - 1 - 12) / windup, (mine - 12) / windup];
        expect(patches(), `at the player's tick ${mine}`).toHaveLength(2);
        expect(filled(), `filled at the player's tick ${mine}`).toBeGreaterThanOrEqual(
          Math.max(0.02, lo) - 1e-3,
        );
        expect(filled(), `filled at the player's tick ${mine}`).toBeLessThanOrEqual(
          Math.min(1, hi) + 1e-3,
        );
      }
      expect(filled(), 'full once the player’s clock is past the hit').toBeCloseTo(1, 3); // the drawn frames, 5 ticks behind, are two thirds in
      for (let tick = hit - LEAD; patches().length > 0; tick++) {
        expect(tick, 'gone once the drawn frames reach the hop’s landing').toBeLessThanOrEqual(
          hit + 400 / 50 + 3,
        );
        hear(tick);
      }
      expect(p.world.resets).toBe(0);
    });

    it('carry a hopping mob on the player’s own clock, so it lands as the world lands it, ahead of the drawn frames', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      const LEAD = 3;
      const first = p.ws
        .frames()
        .filter((f) => f.t === 'input')
        .at(-1)!.seq;
      const hit = 12 + 600 / 50;
      const travel = 400 / 50;
      const sent = (tick: number) => 6 - Math.max(0, Math.min(1, (tick - hit) / travel)); // where the world has it: to 5 over its travel
      const hear = (tick: number) => {
        const ackSeq = first + tick - 11 - LEAD;
        p.push(
          'delta',
          { mobs: [mob(1, 'green_slime', sent(tick))], players: [standing(4, 20)], ackSeq },
          { tick },
        );
        p.frames(1);
      };
      hear(11);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 12 },
      );
      for (let tick = 12; tick + LEAD + 1 <= hit + travel; tick++) {
        hear(tick);
      }
      const slime = p.root('quaternius/GreenBlob.glb')!;
      expect(slime.position.x).toBeCloseTo(5, 1); // landed on the player's clock
      expect(sent(hit + travel - LEAD - 1 - 2)).toBeGreaterThan(5.3); // where the drawn frames, two ticks behind the newest, have it
      expect(p.world.resets).toBe(0);
    });

    it('leave nothing behind when the map goes while their model still loads', async () => {
      const rejected: unknown[] = [];
      const onRejection = (e: unknown) => rejected.push(e);
      process.on('unhandledRejection', onRejection);
      try {
        const p = await playing(FLAT, 4, 20);
        const make = vi.spyOn(Figures.prototype, 'make');
        frames(p, 2, 4, [mob(1, 'green_slime', 6)]);
        const warn = vi.spyOn(console, 'warn');
        leave(p);
        expect(make).toHaveBeenCalled();
        await Promise.allSettled(make.mock.results.map((r) => r.value as Promise<unknown>)); // its load settles
        await new Promise((r) => setTimeout(r, 0)); // and what waits on it runs
        expect(rejected).toEqual([]);
        expect(warn).not.toHaveBeenCalledWith(expect.stringContaining('didn’t load'));
        expect(warn).not.toHaveBeenCalledWith(expect.stringContaining("didn't load"));
      } finally {
        process.off('unhandledRejection', onRejection);
      }
    });

    it('say once when their model fails to load, and draw the rest', async () => {
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
      broken = 'quaternius/GreenBlob.glb';
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [
        mob(1, 'green_slime', 6),
        mob(2, 'green_slime', 8),
        mob(3, 'wild_boar', 10),
      ]);
      await vi.waitFor(() =>
        expect(warn).toHaveBeenCalledWith(
          expect.stringContaining("quaternius/GreenBlob.glb didn't load"),
        ),
      );
      expect(warn.mock.calls.filter(([m]) => String(m).includes('GreenBlob'))).toHaveLength(1);
      await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    });

    it('end an attack’s look when a blow breaks off its windup', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 15, [mob(1, 'green_slime', 6)]);
      expect(p.scene()!.meshes.filter((m) => m.name === 'patch')).toHaveLength(2);
      frames(p, 16, 20, [mob(1, 'green_slime', 6, MobPose.FLINCH)]);
      expect(p.scene()!.meshes.filter((m) => m.name === 'patch')).toHaveLength(0);
    });
  });

  describe('hits', () => {
    it('spark on the mob struck and pop a number over it: a crit gold and bigger, a miss grey', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      const sparks = () => p.scene()!.transformNodes.filter((n) => n.name === 'spark');
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 }, { tick: 11 });
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 14, crit: true }, { tick: 11 });
      p.push('damage', { source: `p:${ME}`, target: 'm:1', miss: true }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([
        ['hit', '7'],
        ['crit', '14!'],
        ['miss', 'MISS'],
      ]);
      expect(sparks()).toHaveLength(4); // a spark each, the crit's burst on top
      const camera = p.scene()!.activeCamera!.globalPosition;
      const boar = mobData.find((m) => m.id === 'wild_boar')!;
      const middle = new Vector3(6, boar.height / 2, 20);
      expect(Vector3.Distance(sparks()[0]!.position, camera)).toBeCloseTo(
        Vector3.Distance(middle, camera) - boar.radius,
        3,
      ); // in front of the boar toward the camera, not inside it
      p.frames(20);
      expect(numbers(p)).toEqual([]);
      expect(sparks()).toHaveLength(0);
    });

    it('spark on a killed mob as it lies, and pop the killing blow’s number over it', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push('delta', { removedMobs: [1] }, { tick: 11 });
      p.frames(6); // drawn gone: lying its second
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([['hit', '7']]);
      const [spark] = p.scene()!.transformNodes.filter((n) => n.name === 'spark');
      const camera = p.scene()!.activeCamera!.globalPosition;
      const boar = mobData.find((m) => m.id === 'wild_boar')!;
      expect(Vector3.Distance(spark!.position, camera)).toBeCloseTo(
        Vector3.Distance(new Vector3(6, boar.height / 2, 20), camera) - boar.radius,
        3,
      ); // sized and placed by the boar it was
    });

    it('spark on a mob killed on the tick it spawned, never drawn, where the frame put it', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push('delta', { mobs: [mob(2, 'wild_boar', 9)], removedMobs: [2] }, { tick: 11 });
      p.push('damage', { source: `p:${ME}`, target: 'm:2', amount: 7 }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([['hit', '7']]);
      const [spark] = p.scene()!.transformNodes.filter((n) => n.name === 'spark');
      const camera = p.scene()!.activeCamera!.globalPosition;
      const boar = mobData.find((m) => m.id === 'wild_boar')!;
      expect(Vector3.Distance(spark!.position, camera)).toBeCloseTo(
        Vector3.Distance(new Vector3(9, boar.height / 2, 20), camera) - boar.radius,
        3,
      );
    });

    it('show a hit on the player in red, over their name', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push('damage', { source: 'm:1', target: `p:${ME}`, amount: 5 }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([['taken', '-5']]);
    });

    it('hold the player’s pose and the mob’s a moment for their own hit, once in 100 ms', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined());
      const held = vi.spyOn(Body.prototype, 'hold');
      key('KeyX', true);
      key('KeyX', false);
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 }, { tick: 11 });
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 }, { tick: 11 });
      p.push('damage', { source: 'p:c-robin', target: 'm:1', amount: 7 }, { tick: 11 });
      const bodies = held.mock.contexts as Body[];
      const on = (file: string) =>
        new Set(
          held.mock.calls
            .filter((_, i) => bodies[i]?.root.name === file)
            .map((c) => JSON.stringify(c[0])),
        );
      const holds = [on(classLook('warrior').file), on('quaternius/GreenBlob.glb')];
      expect(holds.map((h) => h.size)).toEqual([1, 1]); // one hold, the second hit inside it
      expect(held).toHaveBeenCalledTimes(4); // none for another player's hit
    });

    it('show a ranged hit as its arrow reaches the mob, not before', async () => {
      const p = await playing(FLAT, 4, 20, 'bowman');
      frames(p, 2, 10, [mob(1, 'wild_boar', 12)]);
      key('KeyX', true);
      key('KeyX', false);
      p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 9 }, { tick: 11 });
      p.frames(3); // 8 m at 43.75 m/s: 183 ms
      expect(numbers(p)).toEqual([]);
      p.frames(1);
      expect(numbers(p)).toEqual([['hit', '9']]);
    });

    it('blink the player while the world says they are invulnerable', async () => {
      const p = await playing(FLAT, 4, 20);
      const shown = () => p.root(classLook('warrior').file) !== undefined;
      p.push('delta', { players: [{ ...standing(4, 20), invulnerable: true }] }, { tick: 2 });
      const seen = [0, 1, 2, 3].map(() => {
        p.frames(1);
        return shown();
      });
      expect(seen).toEqual([false, true, true, false]); // hidden the first 100 ms of every 200
      p.push('delta', { players: [standing(4, 20)] }, { tick: 3 });
      p.frames(1);
      expect(shown()).toBe(true);
    });

    it('show what a potion restored over the player, and a level-up', async () => {
      const p = await playing(FLAT, 4, 20);
      combatLog.getState().drank('red_potion', 30);
      p.push('levelUp', { character: { id: ME, level: 4 } }, { tick: 2 });
      p.frames(1);
      expect(numbers(p)).toEqual([
        ['healHp', '+30'],
        ['crit', 'LEVEL UP!'],
      ]);
    });
  });

  it('draws a drop where it fell and flies it into the player who collects it', async () => {
    const p = await playing(FLAT, 4, 20);
    const drops = () => p.scene()!.meshes.filter((m) => m.name === 'drop').length;
    p.push(
      'delta',
      { drops: [{ id: 3, template: 'slime_gel', quantity: 1, x: 5, y: 0, z: 20, ownerId: ME }] },
      { tick: 2 },
    );
    p.frames(1);
    expect(drops()).toBe(1);
    p.push('delta', { collectedDrops: [3] }, { tick: 3 });
    p.frames(3);
    expect(drops()).toBe(1); // on its way
    p.frames(5);
    expect(drops()).toBe(0);
  });

  it('attacks with the attack key, aimed along the heading, and lets a skill cut it at its cancel point, leaving the cut pose behind', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 3,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); // the press waits for the cancel point on a timer
    const me = p.root(classLook('warrior').file);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack').map((d) => d.aim)).toEqual([expect.closeTo(Math.PI / 2, 5)]);
    p.frames(2);
    expect(weight(me, '1H_Melee_Attack_Slice_Diagonal')).toBe(1);
    const attack = classes.find((c) => c.id === 'warrior')!.attack;
    const cancelMs =
      actionPoints(ticksIn(attack.length_ms, FRAME_MS), attack.hit, attack.cancel).cancel *
      FRAME_MS;
    const still = () =>
      p.scene()!.transformNodes.find((n) => n.name === `${classLook('warrior').file} still`);
    expect(still()?.isEnabled()).toBe(false); // made ahead, unseen: an attack alone leaves none
    key('Digit1', true);
    key('Digit1', false);
    let waited = 0;
    while (p.sent('useSkill').length === 0 && waited++ < 20) {
      p.frames(1);
    }
    expect(still()?.isEnabled()).toBe(true); // the cut pose, left behind
    p.frames(Math.ceil(cancelMs / FRAME_MS) + Math.ceil(BLEND_MS / FRAME_MS) + 1 - waited);
    expect(p.sent('useSkill')).toEqual([expect.objectContaining({ skill: 'strike' })]);
    expect([
      weight(me, '1H_Melee_Attack_Slice_Diagonal'),
      weight(me, '1H_Melee_Attack_Chop'),
    ]).toEqual([0, 1]);
    p.frames(6);
    expect(still()?.isEnabled()).toBe(false); // faded in 300 ms
  });

  it('glows round the player while a teleport channels; a movement key, an attack or a skill starting ends it at once', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 3,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    const glowing = () =>
      p.scene()!.meshes.filter((m) => m.name === 'ring' && m.isEnabled() && m.visibility > 0)
        .length;
    const channel = () => {
      teleport.getState().start('cave');
      p.live(1);
      expect(glowing()).toBe(2);
    };
    const ended = () => {
      expect(teleport.getState().to).toBeNull();
      p.live(1);
      expect(glowing()).toBe(0);
    };
    channel();
    p.live(10);
    expect([teleport.getState().to, glowing()]).toEqual(['cave', 2]);
    key('ArrowRight', true);
    ended();
    key('ArrowRight', false);

    channel();
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1);
    ended();

    p.live(40); // the attack over, the world's frames coming
    channel();
    key('Digit1', true);
    key('Digit1', false);
    for (let waited = 0; p.sent('useSkill').length === 0 && waited < 20; waited++) {
      p.live(1);
    }
    expect(p.sent('useSkill')).toHaveLength(1);
    ended();
  });

  it('sends no attack or skill while a stun holds the player, as the world would refuse them, and attacks again once its end tick passes', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 3,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    p.push(
      'delta',
      {
        players: [
          { ...standing(4, 20), statuses: [create(StatusViewSchema, { id: 'stun', ends: 30 })] },
        ],
      },
      { tick: 2 },
    );
    p.frames(1);
    for (const code of ['KeyX', 'Digit1']) {
      key(code, true);
      key(code, false);
    }
    p.frames(2);
    expect([p.sent('attack'), p.sent('useSkill')]).toEqual([[], []]);
    p.push('delta', {}, { tick: 30 }); // the stun's end
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1);
  });

  it('ends a swing a stun cuts off, as the world ends it: its look goes at once, and a skill press waiting for its cancel point is dropped, unsent and unsaid', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 3,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); // the press waits for the cancel point on a timer
    const me = p.root(classLook('warrior').file);
    key('KeyX', true);
    key('KeyX', false);
    p.frames(2);
    key('Digit1', true);
    key('Digit1', false);
    expect([
      p.sent('attack').length,
      p.sent('useSkill'),
      weight(me, '1H_Melee_Attack_Slice_Diagonal'),
    ]).toEqual([1, [], 1]);
    p.push(
      'delta',
      {
        players: [
          { ...standing(4, 20), statuses: [create(StatusViewSchema, { id: 'stun', ends: 60 })] },
        ],
      },
      { tick: 2 },
    );
    p.frames(Math.ceil(BLEND_MS / FRAME_MS) + 1);
    expect(weight(me, '1H_Melee_Attack_Slice_Diagonal')).toBe(0);
    const attack = classes.find((c) => c.id === 'warrior')!.attack;
    p.frames(Math.ceil(ticksIn(attack.length_ms, FRAME_MS) * attack.cancel) + 5); // past its cancel point
    expect(p.sent('useSkill')).toEqual([]);
    expect(toasts.getState().toasts.map((t) => t.text)).not.toContain('You can’t act right now.'); // pressed before the stun, it isn't refused after
  });

  it('leaves a teleport channelling at the attack key in a town, which starts no attack', async () => {
    const p = await playing('town', 14, 16);
    teleport.getState().start('forest');
    key('KeyX', true);
    key('KeyX', false);
    p.frames(2);
    expect([p.sent('attack'), teleport.getState().to]).toEqual([[], 'forest']);
  });

  it('M opens the map window and closes it again', async () => {
    await playing(FLAT, 10, 20);
    key('KeyM', true);
    key('KeyM', false);
    expect(ui.getState().windows).toEqual(['map']);
    key('KeyM', true);
    key('KeyM', false);
    expect(ui.getState().windows).toEqual([]);
  });

  it('tells the map views the map, and about ten times a second the player, the party members here, the lock and the field boss while it’s up', async () => {
    const p = await playing(FLAT, 10, 20);
    expect([mapView.getState().mapId, mapView.getState().size]).toEqual([FLAT, [72, 40]]);
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: ME,
          members: [
            { characterId: ME, name: 'Ayla', mapId: FLAT, online: true },
            { characterId: 'c-robin', name: 'Robin', mapId: FLAT, online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: 'cave', online: true },
          ],
        }),
      }),
    );
    const pack = [mob(1, 'green_slime', 13), mob(2, 'wild_boar', 4)];
    for (let tick = 2; tick <= 10; tick++) {
      p.push(
        'delta',
        { mobs: pack, ...(tick === 2 && { players: [standing(16, 22, 'c-robin', 'thief')] }) },
        { tick },
      );
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    frames(p, 11, 12, pack);
    const marks = () => {
      const m = mapView.getState();
      return {
        me: m.me && [m.me.x, m.me.z],
        party: m.party.map((q) => [q.id, q.x, q.z]),
        lock: m.lock,
        boss: m.boss,
      };
    };
    expect(marks()).toEqual({
      me: [10, 20],
      party: [['c-robin', 16, 22]], // Jade is on another map
      lock: { x: 13, z: 20 },
      boss: null,
    });
    key('Tab', true);
    key('Tab', false);
    frames(p, 13, 14, pack);
    expect(marks().lock).toEqual({ x: 4, z: 20 });
    fieldBoss
      .getState()
      .applyFieldBoss(
        create(FieldBossSchema, { mobId: 1, type: 'green_slime', leavesInMs: 60000n }),
      );
    frames(p, 15, 16, pack);
    expect(marks().boss).toEqual({ x: 13, z: 20 });
    fieldBoss.getState().applyGone(create(FieldBossGoneSchema, { mobId: 1, beaten: true }));
    frames(p, 17, 18, pack);
    expect(marks().boss).toBeNull();
  });

  /** How far yaw a is from yaw b, round the circle. */
  const off = (a: number, b: number) => Math.abs(Math.atan2(Math.sin(a - b), Math.cos(a - b)));

  /** Whether the body drawn from file is in the outline's layer. */
  const outlined = (p: Playing, file: string) => {
    const layer = p.scene()!.effectLayers[0] as HighlightLayer;
    return p
      .root(file)!
      .getChildMeshes()
      .some((m) => layer.hasMesh(m));
  };

  it('locks the nearest mob, behind included, keeps it as another comes closer, outlines it, and aims at it out of its reach', async () => {
    const p = await playing(FLAT, 10, 20);
    frames(p, 2, 10, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 18)]); // 6 m behind, 8 m ahead
    await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    frames(p, 11, 20, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 13)]); // the boar 3 m ahead now
    expect([
      outlined(p, 'quaternius/GreenBlob.glb'),
      outlined(p, 'quaternius/Blob_Dog.glb'),
    ]).toEqual([true, false]);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack').map((d) => d.aim)).toEqual([expect.closeTo(-Math.PI / 2, 5)]); // west, past the sword's reach
  });

  it('Tab locks the next mob, the ring and the outline going with it: a fresh press only, the game’s and not the browser’s, and the chat’s while typing', async () => {
    const p = await playing(FLAT, 10, 20);
    const pack = [mob(1, 'green_slime', 13), mob(2, 'wild_boar', 4)]; // 3 m ahead, 6 m behind
    frames(p, 2, 10, pack);
    await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    frames(p, 11, 12, pack);
    const ring = p.scene()!.getMeshByName('lock ring')!;
    const ringAt = () => (ring.isEnabled() ? [ring.position.x, ring.position.z] : null);
    expect([ringAt(), outlined(p, 'quaternius/GreenBlob.glb')]).toEqual([[13, 20], true]);
    const ghost = p.scene()!.getMeshByName('lock ring ghost') as Mesh; // shown faintly where the place hides it
    expect([
      ghost.parent === ring,
      ghost.geometry === (ring as Mesh).geometry,
      ghost.alphaIndex === ring.alphaIndex,
    ]).toEqual([true, true, true]);
    expect(target.getState().target).toEqual({ mobId: 1, type: 'green_slime', hp: 30 });
    const tab = (more: KeyboardEventInit = {}, on: EventTarget = window) => {
      const e = new KeyboardEvent('keydown', {
        code: 'Tab',
        cancelable: true,
        bubbles: true,
        ...more,
      });
      on.dispatchEvent(e);
      window.dispatchEvent(new KeyboardEvent('keyup', { code: 'Tab' }));
      return e.defaultPrevented;
    };
    expect(tab()).toBe(true);
    frames(p, 13, 13, pack);
    expect([ringAt(), outlined(p, 'quaternius/Blob_Dog.glb')]).toEqual([[4, 20], true]);
    expect(outlined(p, 'quaternius/GreenBlob.glb')).toBe(false);
    expect(target.getState().target?.mobId).toBe(2); // the window with it
    frames(p, 14, 14, [pack[0]!, { ...pack[1]!, hp: 12 }]);
    expect(target.getState().target?.hp).toBe(12);
    tab({ repeat: true }); // a held Tab doesn't run through the pack
    frames(p, 15, 15, pack);
    expect(ringAt()).toEqual([4, 20]);
    const chat = document.createElement('input');
    document.body.appendChild(chat);
    expect(tab({}, chat)).toBe(false);
    chat.remove();
    frames(p, 16, 16, pack);
    expect(ringAt()).toEqual([4, 20]);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack').map((d) => [d.aim, d.target])).toEqual([
      [expect.closeTo(-Math.PI / 2, 5), 2],
    ]); // at the new lock, which the world hits before the nearer slime
    const first = ring.getVerticesData('position')!;
    const angle = Math.atan2(first[0]!, first[2]!);
    p.frames(20); // a second: a quarter turn
    const later = ring.getVerticesData('position')!;
    expect(off(Math.atan2(later[0]!, later[2]!), angle + Math.PI / 2)).toBeLessThan(1e-3);
    expect(tab()).toBe(true);
    frames(p, 17, 17, pack);
    expect(ringAt()).toEqual([13, 20]); // round to the nearest after the farthest
    p.push('delta', { removedMobs: [1, 2] }, { tick: 18 });
    p.frames(25);
    expect([ringAt(), target.getState().target]).toEqual([null, null]); // nothing to lock
  });

  it('locks only mobs in view and holds a lock to the canvas’s edge: a slime south at the bottom edge is on the canvas, inside its margin', async () => {
    const p = await playing(FLAT, 10, 20);
    frames(p, 2, 4, []);
    const scene = p.scene()!;
    const engine = scene.getEngine();
    const middle = mobById('green_slime')!.height / 2;
    const down = (z: number) => {
      const viewport = scene.activeCamera!.viewport.toGlobal(
        engine.getRenderWidth(),
        engine.getRenderHeight(),
      );
      const at = Vector3.Project(
        new Vector3(10, middle, z),
        Matrix.Identity(),
        scene.getTransformMatrix(),
        viewport,
      );
      return at.y / engine.getRenderHeight(); // the share of the canvas down from its top
    };
    let edge = 20; // the first spot south of the player out of view, inside the bottom 8%
    while (down(edge) <= 0.92) {
      edge -= 0.05;
    }
    let off = edge; // the first spot off the canvas
    while (down(off) <= 1) {
      off -= 0.05;
    }
    expect(edge - off).toBeGreaterThan(0.3); // a band to stand a slime in
    const slime = (z: number) => ({ ...mob(1, 'green_slime', 10), z });
    const boar = { ...mob(2, 'wild_boar', 10), z: 26 }; // 6 m north, in view
    const locked = () => target.getState().target?.mobId ?? null;
    frames(p, 5, 6, [slime((edge + off) / 2)]);
    await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
    frames(p, 7, 8, [slime((edge + off) / 2)]);
    expect(locked()).toBeNull(); // the nearest mob, but at the edge
    frames(p, 9, 10, [slime((edge + off) / 2), boar]);
    expect(locked()).toBe(2); // the farther one in view
    frames(p, 11, 12, [slime(18), boar]);
    key('Tab', true);
    key('Tab', false);
    frames(p, 13, 14, [slime(18), boar]);
    expect(locked()).toBe(1);
    frames(p, 15, 16, [slime((edge + off) / 2), boar]);
    expect(locked()).toBe(1); // held at the edge
    frames(p, 17, 18, [slime(off - 0.5), boar]);
    expect(locked()).toBe(2); // off the canvas: dropped, and the one in view locked
  });

  it('turns the player to the lock for an attack’s or a skill’s strike only, the slash left on the lock', async () => {
    const p = await playing(FLAT, 10, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 3,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    const north = { ...mob(1, 'green_slime', 10), z: 26 }; // 6 m north, the heading east
    frames(p, 2, 10, [north]);
    await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
    frames(p, 11, 12, [north]);
    const me = p.root(classLook('warrior').file)!;
    const attack = beatsOf(attackTiming(undefined, 'warrior', FRAME_MS), 0, FRAME_MS);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack').map((d) => [d.aim, d.target])).toEqual([[expect.closeTo(0, 5), 1]]);
    p.frames(Math.ceil(attack.strikeEnd / FRAME_MS) - 1);
    expect(off(me.rotation.y, 0)).toBeLessThan(0.2); // still striking: toward the lock
    p.frames(4);
    expect(off(me.rotation.y, Math.PI / 2)).toBeLessThan(0.05); // struck: back along the heading
    const slash = p.scene()!.transformNodes.find((n) => n.name === 'band');
    expect(slash && off(slash.rotation.y, 0)).toBeLessThan(1e-6); // the slash still where the hit went
    p.frames(Math.ceil(attack.end / FRAME_MS));
    const skill = beatsOf(skillTiming(skillById('strike')!.stages[0]!, FRAME_MS), 0, FRAME_MS);
    key('Digit1', true);
    key('Digit1', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.aim, d.target])).toEqual([
      ['strike', expect.closeTo(0, 5), 1],
    ]);
    p.frames(Math.ceil(skill.strikeEnd / FRAME_MS) - 1);
    expect(off(me.rotation.y, 0)).toBeLessThan(0.2);
    p.frames(4);
    expect(off(me.rotation.y, Math.PI / 2)).toBeLessThan(0.05); // the rest of its second along the heading
  });

  it('a key offers its chain’s follow-up as the world would take it, naming its root: Strike’s is Charge at a far lock, Roll Back’s Lunge after the player’s own dodge, from the newest frame’s ack', async () => {
    const p = await playing(FLAT, 10, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 20,
          hp: 50,
          maxHp: 50,
          sheet: {
            skills: { unspent: 0, invested: { strike: 1, charge: 1, roll_back: 1, lunge: 1 } },
          },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike', 'roll_back']);
    const far = { ...mob(1, 'green_slime', 10), z: 26 }; // 6 m north, past Charge's 3
    frames(p, 2, 10, [far]);
    await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
    frames(p, 11, 12, [far]);
    expect(skillBar.getState().live.slice(0, 2)).toEqual(['charge', 'roll_back']);
    key('Digit1', true);
    key('Digit1', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.chainRoot, d.target])).toEqual([
      ['charge', 'strike', 1],
    ]);
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('strike'); // Charge cooling down
    const acked = p.inputs() - 1;
    p.push('delta', { ackSeq: acked, mobs: [far] }, { tick: 13 });
    p.push('damage', { source: 'm:1', target: 'p:c-robin', dodge: true }, { tick: 13 }); // another's
    p.frames(1);
    expect(skillBar.getState().live[1]).toBe('roll_back');
    p.push('damage', { source: 'm:1', target: `p:${ME}`, dodge: true }, { tick: 13 });
    p.frames(1);
    expect(skillBar.getState().live[1]).toBe('lunge');
    while (p.inputs() < acked + 30) {
      p.frames(1); // to the window's last input, 1.5 s on from the acked one
    }
    expect(skillBar.getState().live[1]).toBe('lunge');
    p.frames(1);
    expect(skillBar.getState().live[1]).toBe('roll_back');
  });

  it('fades the player over their own skill’s dodge from its press, and another player while their frames say they dodge, behind the newest as they are drawn', async () => {
    const p = await playing(FLAT, 10, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 20,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { roll_back: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['roll_back']);
    const faded = (file: string) =>
      p
        .root(file)!
        .getChildMeshes(false)
        .some((m) => m.material?.name.endsWith(` ${FADED}`));
    const warrior = classLook('warrior').file;
    const thief = classLook('thief').file;
    let tick = 1;
    const frame = (dodging: boolean) => {
      p.push(
        'delta',
        { players: [{ ...standing(16, 20, 'c-robin', 'thief'), dodging }] },
        { tick: ++tick },
      );
      p.frames(1);
    };
    frame(false);
    await vi.waitFor(() => expect(p.root(thief)).toBeDefined(), WAIT);
    frame(false);
    frame(false);
    expect([faded(warrior), faded(thief)]).toEqual([false, false]);
    key('Digit1', true);
    key('Digit1', false);
    expect(p.sent('useSkill').map((d) => d.skill)).toEqual(['roll_back']);
    const mine = [...Array(9).keys()].map(() => {
      frame(false);
      return faded(warrior);
    });
    expect(mine).toEqual([true, true, true, true, true, true, true, false, false]); // 400 ms of 50 ms frames
    const theirs = [true, true, true, false, false, false, false].map((d) => {
      frame(d);
      return faded(thief);
    });
    expect(theirs).toEqual([false, true, true, true, false, false, false]); // drawn 100 ms behind the newest: each frame shows the tick before the one it got
  });

  it('a dodge the world refuses stops fading the player', async () => {
    const p = await playing(FLAT, 10, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 20,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { roll_back: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['roll_back']);
    const warrior = classLook('warrior').file;
    await vi.waitFor(() => expect(p.root(warrior)).toBeDefined(), WAIT);
    const faded = () =>
      p
        .root(warrior)!
        .getChildMeshes(false)
        .some((m) => m.material?.name.endsWith(` ${FADED}`));
    key('Digit1', true);
    key('Digit1', false);
    p.frames(1);
    expect(faded()).toBe(true);
    const req = p.ws.frames().findLast((f) => f.t === 'useSkill')!.req;
    p.push('response', { ok: false, code: ERR.SKILL_COOLING_DOWN }, { req });
    await Promise.resolve();
    p.frames(1);
    expect(faded()).toBe(false);
  });

  it('turns another player to their attack’s or skill’s aim for its strike only', async () => {
    const p = await playing(FLAT, 4, 20);
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', tick === 2 ? { players: [standing(14, 20, 'c-robin', 'thief')] } : {}, {
        tick,
      });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
    p.frames(2);
    const robin = p.root(classLook('thief').file)!;
    const ticks = ticksIn(classes.find((c) => c.id === 'thief')!.attack.length_ms, FRAME_MS);
    const attack = beatsOf(attackIn('thief', ticks), 0, FRAME_MS);
    p.push('attackUsed', { attacker: 'c-robin', lengthTicks: ticks, aim: 0 }, { tick: 9 });
    p.frames(Math.ceil(attack.strikeEnd / FRAME_MS) - 1);
    expect(off(robin.rotation.y, 0)).toBeLessThan(0.2);
    p.frames(4);
    expect(off(robin.rotation.y, Math.PI / 2)).toBeLessThan(0.05);
    p.frames(Math.ceil(attack.end / FRAME_MS));
    const skill = beatsOf(skillTiming(skillById('double_stab')!.stages[0]!, FRAME_MS), 0, FRAME_MS);
    p.push(
      'skillUsed',
      { caster: 'c-robin', skill: 'double_stab', stage: 0, aim: 0 },
      { tick: 30 },
    );
    p.frames(Math.ceil(skill.strikeEnd / FRAME_MS) - 1);
    expect(off(robin.rotation.y, 0)).toBeLessThan(0.2);
    p.frames(4);
    expect(off(robin.rotation.y, Math.PI / 2)).toBeLessThan(0.05);
  });

  it('lets go of the lock at a death, and picks the nearest again after a respawn here', async () => {
    const p = await playing(FLAT, 10, 20);
    frames(p, 2, 10, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 18)]);
    await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    frames(p, 11, 20, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 13)]);
    expect(outlined(p, 'quaternius/GreenBlob.glb')).toBe(true);
    p.push('died', { characterId: ME, x: 10, y: 0, z: 20 }, { tick: 21 });
    frames(p, 22, 23, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 13)]);
    expect(outlined(p, 'quaternius/GreenBlob.glb')).toBe(false); // nothing outlined on the dead
    p.push('delta', { players: [standing(10, 20)], ackSeq: 0 }, { tick: 24 });
    p.push('respawned', { characterId: ME, mapId: FLAT, x: 10, y: 0, z: 20, hp: 50 }, { tick: 24 });
    frames(p, 25, 27, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 13)]);
    expect([
      outlined(p, 'quaternius/GreenBlob.glb'),
      outlined(p, 'quaternius/Blob_Dog.glb'),
    ]).toEqual([false, true]);
  });

  it('heals along the heading, aiming at no mob, not even one its body overlaps', async () => {
    const p = await playing(FLAT, 4, 20, 'magician');
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'magician',
          level: 10,
          hp: 40, // hurt, so the heal finds someone
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { heal: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['heal']);
    frames(p, 2, 10, [{ ...mob(1, 'green_slime', 4.3), z: 20.3 }]); // over the player, 45° off their heading
    key('Digit1', true);
    key('Digit1', false);
    const [sent] = p.sent('useSkill');
    expect([sent?.skill, sent?.aim]).toEqual(['heal', expect.closeTo(Math.PI / 2, 5)]);
  });

  it.each([
    ['its frame first, as the world sends a tick', true],
    ['its frame after', false],
  ])(
    'lays the player down when they die, and stands them where the world puts them on a respawn here, %s',
    async (_, frameFirst) => {
      const p = await playing(FLAT, 4, 20);
      p.push('died', { characterId: ME, x: 4, y: 0, z: 20 }, { tick: 2 });
      p.frames(10);
      const body = p.root(classLook('warrior').file);
      expect(weight(body, 'Death_A')).toBe(1);
      const sent = p.inputs();
      p.frames(5);
      expect(p.inputs()).toBe(sent); // the server holds a dead player
      const frame = () => p.push('delta', { players: [standing(6, 21)], ackSeq: 0 }, { tick: 3 });
      if (frameFirst) {
        frame();
      }
      p.push('respawned', { characterId: ME, mapId: FLAT, x: 6, y: 0, z: 21, hp: 50 }, { tick: 3 });
      if (!frameFirst) {
        frame();
      }
      p.frames(1);
      expect([body!.position.x, body!.position.z]).toEqual([6, 21]);
      p.frames(10);
      expect(weight(body, 'Death_A')).toBe(0);
      expect(p.inputs()).toBeGreaterThan(sent);
    },
  );

  describe('statuses', () => {
    /** The colour 0xrrggbb as the DOM writes it. */
    const rgb = (c: number) => `rgb(${(c >> 16) & 0xff}, ${(c >> 8) & 0xff}, ${c & 0xff})`;

    it('marks each unit’s statuses as the newest frames list them, the player’s own, another’s and a mob’s, each until its end tick or its frame drops it', async () => {
      const plays: { id: string; on: () => Unit | undefined; stopped: boolean }[] = [];
      const spy = vi.spyOn(Fx.prototype, 'play').mockImplementation((id, o) => {
        const m = { id, on: () => o.follow?.(), stopped: false };
        plays.push(m);
        return () => {
          m.stopped = true;
        };
      });
      const marks = () => plays.filter((m) => m.id.startsWith('status.'));
      try {
        const p = await playing(FLAT, 4, 20);
        const on = (id: string, ends: number) => [create(StatusViewSchema, { id, ends })];
        const robin = { ...standing(9, 20, 'c-robin'), statuses: on('slow', 50) };
        const send = (tick: number, me: PlayerWire, slime: MobWire) => {
          p.push('delta', { players: [me, robin], mobs: [slime] }, { tick });
          p.frames(1);
        };
        const stunned = { ...standing(4, 20), statuses: on('stun', 30) };
        const poisoned = { ...mob(1, 'green_slime', 6), statuses: on('poison', 40) };
        for (let tick = 2; tick <= 12; tick++) {
          send(tick, stunned, poisoned);
        }
        const xs = () =>
          marks()
            .filter((m) => !m.stopped)
            .map((m) => [m.id, Math.round(m.on()?.x ?? NaN)]);
        expect(xs()).toEqual([
          ['status.stun', 4],
          ['status.slow', 9],
          ['status.poison', 6],
        ]);
        send(13, { ...standing(4, 20), statuses: on('stun', 35) }, poisoned); // the same stun, longer
        expect(marks()).toHaveLength(3);
        send(14, stunned, { ...mob(1, 'green_slime', 6), statuses: [] }); // its poison ended
        expect(xs()).toEqual([
          ['status.stun', 4],
          ['status.slow', 9],
        ]);
        p.push('delta', {}, { tick: 30 }); // the stun's end, though no frame of the player says so
        p.frames(1);
        expect(xs()).toEqual([['status.slow', 9]]);
        expect(marks()).toHaveLength(3);
      } finally {
        spy.mockRestore();
      }
    });

    it('pops a damage-over-time tick small in its status’s colour, with no spark or hold, leaving its inflicter’s windup to its own hit', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 16, [mob(1, 'green_slime', 6)]);
      const telegraphs = () =>
        p.scene()!.meshes.filter((m) => m.name === 'patch' && m.visibility > 0).length;
      expect(telegraphs()).toBe(2);
      const held = vi.spyOn(Body.prototype, 'hold');
      try {
        p.push(
          'damage',
          { source: 'm:1', target: `p:${ME}`, amount: 2, statusId: 'poison' },
          { tick: 16 },
        );
        p.push(
          'damage',
          { source: `p:${ME}`, target: 'm:1', amount: 3, statusId: 'burn' },
          { tick: 16 },
        );
        frames(p, 17, 17, [mob(1, 'green_slime', 6)]);
        expect(numbers(p)).toEqual([
          ['dot', '-2'],
          ['dot', '3'],
        ]);
        const shown = [...p.parent.querySelectorAll<HTMLElement>('[data-number]')];
        expect(shown.map((e) => e.style.color)).toEqual([
          rgb(statusColor('poison')),
          rgb(statusColor('burn')),
        ]);
        expect(p.scene()!.transformNodes.filter((n) => n.name === 'spark')).toHaveLength(0);
        expect(held).not.toHaveBeenCalled();
        expect(telegraphs()).toBe(2); // the hop still to land
      } finally {
        held.mockRestore();
      }
    });

    it('leaves a spit in flight to its own end when its mob’s damage over time ticks meanwhile', async () => {
      const p = await playing(FLAT, 4, 20);
      const bat = [mob(1, 'cave_bat', 10)];
      frames(p, 2, 10, bat);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 4, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      const balls = () =>
        p.scene()!.meshes.filter((m) => m.name === 'ball' && m.isEnabled()).length;
      frames(p, 11, 25, bat); // thrown at tick 23, 0.8 s to fly
      expect(balls()).toBe(1);
      p.push(
        'damage',
        { source: 'm:1', target: `p:${ME}`, amount: 1, statusId: 'poison' },
        { tick: 25 },
      );
      frames(p, 26, 27, bat);
      expect(balls()).toBe(1); // a tick isn't the spit landing
    });

    it('edges a mob attack’s area in the colour of the status it inflicts', async () => {
      const p = await playing(FLAT, 4, 20);
      const pack = [mob(1, 'orange_mushroom', 7), mob(2, 'green_slime', 4.5)];
      frames(p, 2, 10, pack);
      const edges = () =>
        p
          .scene()!
          .meshes.filter((m) => m.name === 'line' && m.isEnabled() && m.visibility > 0)
          .map((m) => (m.material as StandardMaterial).emissiveColor.toHexString().toLowerCase());
      p.push(
        'mobAttacked',
        { mob: 2, attack: 0, yaw: Math.PI / 2, toX: 5.5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 16, pack);
      expect(edges()).toEqual([]); // the slime's hop inflicts nothing
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 4, toY: 0, toZ: 20 },
        { tick: 17 },
      );
      frames(p, 17, 22, pack);
      expect(edges()).toEqual([`#${statusColor('poison').toString(16).padStart(6, '0')}`]);
    });
  });

  it('walks on the world’s own frames, each input acked with the state its sim gave, and never resets', async () => {
    const p = await playing('forest', 4, 20);
    const g = (await loadMap('forest')).geometry;
    const steps = tickTiming(RATE, movement.dt).stepsPerTick;
    let server: State = { x: 4, y: 0, z: 20, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 1, hz: 0 };
    let ran = 0;
    let tick = 1;
    key('ArrowRight', true);
    for (let i = 0; i < 60; i++) {
      if (i === 20) {
        key('ArrowUp', true);
        key('Space', true);
      }
      if (i === 24) {
        key('Space', false);
      }
      if (i === 40) {
        key('ArrowRight', false);
        key('ArrowUp', false);
      }
      p.frames(1);
      const inputs = p.ws.frames().filter((f) => f.t === 'input');
      while (ran < inputs.length - 2) {
        const f = inputs[ran++]!; // each Delta two ticks behind its input, as on a short trip
        for (let k = 0; k < steps; k++) {
          server = step(server, f.d as unknown as Parameters<typeof step>[1], g, movement);
        }
        p.push('delta', { players: [wireOf(server)], ackSeq: f.seq }, { tick: ++tick });
      }
    }
    expect(server.x).toBeGreaterThan(8); // it walked, jumped and turned
    expect(p.world.resets).toBe(0);
  });

  it.each([
    ['standing', -1],
    ['mid-swing, the press held till the attack’s cancel point', 3],
  ])(
    'sends a moving skill’s way at the lock, %s, and predicts its dash from the input after the sent press with no correction',
    async (_, attackAt) => {
      const strike = skillById('strike')!.stages[0] as { move: unknown }; // no shipped skill moves yet
      const was = strike.move;
      strike.move = { kind: 'dash', distance: 4, time_ms: 200, toward: 'lock', stop: true };
      try {
        const p = await playing(FLAT, 10, 20);
        character.getState().applyWelcome(
          create(WelcomeSchema, {
            token: 'tok-1',
            mapId: FLAT,
            character: {
              id: ME,
              name: 'Ayla',
              class: 'warrior',
              level: 3,
              hp: 50,
              maxHp: 50,
              sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
            },
          }),
        );
        energy.getState().welcome(10);
        skillBar.getState().load(['strike']);
        const north = { ...mob(1, 'green_slime', 10), z: 30 }; // 10 m north: past the dash's 4
        frames(p, 2, 10, [north]);
        await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
        const g = (await loadMap(FLAT)).geometry;
        const steps = tickTiming(RATE, movement.dt).stepsPerTick;
        let server: State = {
          x: 10,
          y: 0,
          z: 20,
          vx: 0,
          vy: 0,
          vz: 0,
          on_ground: true,
          hx: 1,
          hz: 0,
        };
        let ran = 0;
        let tick = 10;
        let pressedAt = -1; // the input under way as the key went down
        vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] }); // a held press goes again on a timer, which the frames run
        for (let i = 0; i < 40; i++) {
          if (i === attackAt) {
            key('KeyX', true);
            key('KeyX', false);
          }
          if (i === 5) {
            pressedAt = p.ws
              .frames()
              .filter((f) => f.t === 'input')
              .at(-1)!.seq;
            key('Digit1', true);
            key('Digit1', false);
          }
          p.frames(1);
          const sent = p.ws.frames();
          const press = sent.find((f) => f.t === 'useSkill');
          const inputs = sent.filter((f) => f.t === 'input');
          while (ran < inputs.length - 2) {
            const f = inputs[ran++]!;
            if (press && f.seq === press.seq + 1) {
              server = dash(server, { x: 0, z: 1, length: 4 }, 4 * steps, true, false, movement);
            }
            for (let k = 0; k < steps; k++) {
              server = step(server, f.d as unknown as Parameters<typeof step>[1], g, movement);
            }
            p.push(
              'delta',
              { players: [wireOf(server)], mobs: [north], ackSeq: f.seq },
              { tick: ++tick },
            );
          }
        }
        expect(p.sent('useSkill')).toMatchObject([
          { skill: 'strike', target: 1, way: { x: 0, z: 1, length: 4 } },
        ]);
        const sentAt = p.ws.frames().find((f) => f.t === 'useSkill')!.seq;
        expect(sentAt > pressedAt + 1).toBe(attackAt >= 0); // mid-swing, sent at the cancel point
        expect(server.z).toBeCloseTo(24, 9); // dashed the skill's 4 m toward the lock
        expect(p.world.resets).toBe(0);
      } finally {
        vi.useRealTimers();
        strike.move = was;
      }
    },
  );

  it('builds the map again after a backoff when it doesn’t load, and once more if another map was named meanwhile', async () => {
    const p = await playing(FLAT, 2, 20);
    vi.spyOn(Math, 'random').mockReturnValue(0); // a retry waits 0 ms
    const served = globalThis.fetch;
    let failures = 1;
    let release: () => void = () => undefined;
    const held = new Promise<void>((r) => (release = r));
    vi.stubGlobal('fetch', (url: string) => {
      if (url.endsWith(`${FLAT2}.json`) && failures-- > 0) {
        return Promise.resolve(new Response(null, { status: 503 }));
      }
      return url.endsWith(`${FLAT2}.json`) ? held.then(() => served(url)) : served(url);
    });
    changeMap(p, { mapId: FLAT2, x: 44, y: 0, z: 16, yaw: 0 });
    await vi.waitFor(
      () =>
        expect(toasts.getState().toasts.map((t) => t.text)).toContain(
          'The map didn’t load. Trying again…',
        ),
      WAIT,
    );
    p.push('mapChange', { mapId: FLAT3, x: 2, y: 0, z: 10, yaw: 0 }, { tick: 3 }); // while FLAT2 is still loading
    p.push('snapshot', { players: [standing(2, 10)] }, { tick: 300 });
    release();
    await vi.waitFor(
      () => expect(p.scene()?.transformNodes.some((n) => n.name === `place ${FLAT3}`)).toBe(true),
      WAIT,
    );
    expect(p.scene()?.transformNodes.some((n) => n.name === `place ${FLAT2}`)).toBe(false);
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
    p.frames(2);
    expect(p.root(classLook('warrior').file)!.position.x).toBe(2);
  });

  it('changes the map on a MapChange: the old one goes under the veil, the new one is built and placed by its Snapshot', async () => {
    const p = await playing(FLAT, 2, 20);
    changeMap(p, { mapId: FLAT2, x: 44, y: 0, z: 16, yaw: -Math.PI / 2 });
    p.push('snapshot', { players: [standing(44, 16)] }, { tick: 300 });
    await vi.waitFor(
      () => expect(p.scene()?.transformNodes.some((n) => n.name === `place ${FLAT2}`)).toBe(true),
      WAIT,
    );
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
    p.frames(2);
    expect(p.root(classLook('warrior').file)!.position.x).toBe(44);
    const before = p.inputs();
    p.frames(4);
    expect(p.inputs()).toBe(before + 4);
  });

  it('draws a knockback the world made as a shove from where the player was drawn, eased out over 100 ms', async () => {
    const p = await playing(FLAT, 4, 20);
    p.frames(2);
    const sent = p.ws
      .frames()
      .filter((f) => f.t === 'input')
      .at(-1)!;
    p.push(
      'delta',
      { players: [{ ...standing(4.5, 20), onGround: false, vy: 6.25 }], ackSeq: sent.seq },
      { tick: 3 },
    );
    const me = p.root(classLook('warrior').file)!;
    const xs = [0, 1, 2].map(() => {
      p.frames(1);
      return me.position.x;
    });
    expect(p.world.resets).toBe(1);
    expect(xs).toEqual([4.5 - 0.5 * 0.25, 4.5, 4.5]); // a quarter of it left 50 ms on, none at 100
  });

  it('keeps walking on a key held through a portal, as the keyboard still holds it', async () => {
    const p = await playing(FLAT, 44, 16);
    key('ArrowRight', true);
    p.frames(2);
    changeMap(p, { mapId: FLAT2, x: 2, y: 0, z: 20, yaw: Math.PI / 2 });
    p.push('snapshot', { players: [standing(2, 20)] }, { tick: 300 });
    await vi.waitFor(
      () => expect(p.scene()?.transformNodes.some((n) => n.name === `place ${FLAT2}`)).toBe(true),
      WAIT,
    );
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
    const before = p.inputs();
    p.frames(4);
    key('ArrowRight', false);
    expect(
      p
        .sent('input')
        .slice(before)
        .map((d) => d.right),
    ).toEqual([true, true, true, true]);
    expect(p.root(classLook('warrior').file)!.position.x).toBeGreaterThan(2);
  });

  it('holds the player and their keys while the world is silent for two seconds, until its frames come back', async () => {
    const p = await playing(FLAT, 4, 20);
    p.frames(42);
    expect(connection.getState().worldStalled).toBe(true);
    const sent = p.inputs();
    key('ArrowRight', true);
    p.frames(5);
    expect(p.inputs()).toBe(sent);
    p.push('delta', {}, { tick: 50 });
    p.frames(5);
    key('ArrowRight', false);
    expect(connection.getState().worldStalled).toBe(false);
    expect(p.inputs()).toBeGreaterThan(sent);
  });
});
