import { readFileSync } from 'node:fs';
import path from 'node:path';

import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import { CascadedShadowGenerator } from '@babylonjs/core/Lights/Shadows/cascadedShadowGenerator';
import type { TextureBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/textureBlock';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import type { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Frustum } from '@babylonjs/core/Maths/math.frustum';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
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
  PartySpotsSchema,
  PartyStateSchema,
  PartyViewSchema,
  StatusViewSchema,
  NpcDialogSchema,
  LookSchema,
  WelcomeSchema,
  MatchPhase,
  MatchStateSchema,
  PouchKind,
  PouchViewSchema,
} from '@/protocol/messages';
import { dash, endSprint, step } from '@/sim/step';
import type { State } from '@/sim/step';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { fighters } from '@/state/fighters';
import { mapView } from '@/state/map-view';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { pouches } from '@/state/pouches';
import { pvp } from '@/state/pvp';
import { quests } from '@/state/quests';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';
import { FakeWebSocket } from '@/testing/fake-socket';
import type { ServerInit } from '@/testing/fake-socket';
import { mapJson, servePublic } from '@/testing/public-on-disk';
import { HOLLOW, TINY, tinyFile } from '@/testing/region-fixture';
import { resetStores } from '@/testing/reset-stores';
import { builtShaders } from '@/testing/shaders';

import { GUARD_M } from '../effects/draw';
import { Fx } from '../effects/fx';
import type { Body as Unit } from '../effects/types';
import { attackIn, attackTiming, skillTiming } from '../systems/actions';
import { Arrival } from '../systems/arrival';
import type { LookWire } from '../systems/interpolation';
import { MobAttacks } from '../systems/mob-attacks';
import { Predictor } from '../systems/prediction';

import { beatsOf } from './beats';
import { BLEND_MS } from './blend';
import { Body, FADED } from './body';
import type { Figure } from './body';
import { buildChunk } from './chunk-build';
import { Figures } from './figures';
import { CHEST_M } from './follow';
import { classLook, mobLook } from './looks';
import type { ModelLook } from './looks';
import { loadMap } from './map';
import { bakeUrl } from './region-bake';
import { Palette } from './toon';
import { World } from './world';

const ME = 'c-ayla';
const RATE = 20;
const FRAME_MS = 50;
const NEAR_MAP_M = 1000; // the test maps' size, well inside the warm's distance
const MODELS = path.resolve(process.cwd(), 'public/models');
const WAIT = { timeout: 3000, interval: 5 }; // polled every 5 ms, not 50; 3 s for a build on a CI runner under coverage

vi.mock('./kit-models', () => ({ kitModels: {} })); // every key a stand-in: place.test draws town and forest with the real kit models

/** Hunting maps with no NPCs, served flat and bare for the tests no map's own layout bears on: FLAT, the two a map change goes to, one served as a region, the widest a block holds, that with a tower, and one served as a cave. */
const [FLAT, FLAT2, FLAT3, REGIONED, WIDE, TOWERED, CAVED] = maps
  .filter((m) => m.kind === 'hunting' && npcsOn(m.id).length === 0)
  .map((m) => m.id) as [string, string, string, string, string, string, string];

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

/** Map id drawn from the tiny region's bake, 128 m square, its ground the bake's as mapjson writes a region, with a gate in its river 3 m below the bank. */
function regioned(id: string) {
  return {
    ...bare(id),
    geometry: undefined,
    portals: [{ name: 'gate', at: [96, 74], radius: 1.2, to: FLAT }],
    region: TINY,
  };
}

/** Map id drawn from the hollow bake, a cave below tiny, as mapjson writes a region. */
function caved(id: string) {
  return { ...bare(id), sky: 'cave', geometry: undefined, portals: [], region: HOLLOW };
}

/** The tiny bake's region.json with a town area besides its field, Market Square: where the server says the player stands is all the client reads of it. */
function townedTiny(): unknown {
  const j = JSON.parse(new TextDecoder().decode(tinyFile('region.json'))) as { areas: unknown[] };
  const square = {
    id: 'square',
    name: 'Market Square',
    area: [],
    levels: [0, 0],
    biome: 'meadow',
    kind: 'town',
  };
  return { ...j, areas: [...j.areas, square] };
}

/** Map id flat and bare, 128 by 64 m: the widest the server sends whole (large-regions.md section 5). */
function wide(id: string) {
  const m = bare(id);
  return {
    ...m,
    geometry: {
      ...m.geometry,
      size_x: 128,
      size_z: 64,
      terrain: { cell: 8, nx: 17, nz: 9, heights: Array<number>(17 * 9).fill(0) },
    },
  };
}

/** Map id as WIDE with a 14 m tower 8.6 m south of (60, 40): at the camera's end of its line to a player there. */
function towered(id: string) {
  const m = wide(id);
  return {
    ...m,
    geometry: {
      ...m.geometry,
      boxes: [{ x: 60, z: 31.4, half_w: 1, half_d: 0.5, ux: 1, uz: 0, top: 14 }], // south of the 8 m cell the player's head is in
    },
    box_models: [{ model: 'tower', yaw: 0 }],
  };
}

/** The maps served in place of the real ones, made once; the rest come from disk. */
const SERVED = {
  [FLAT]: bare(FLAT),
  [FLAT2]: bare(FLAT2),
  [FLAT3]: bare(FLAT3),
  [REGIONED]: regioned(REGIONED),
  [CAVED]: caved(CAVED),
  [`regions/${TINY.id}/${TINY.hash}/region.json`]: townedTiny(),
  [WIDE]: wide(WIDE),
  [TOWERED]: towered(TOWERED),
  town: slim('town'),
  ring: { ...bare('ring'), prep_wall: (mapJson('ring') as { prep_wall: unknown }).prep_wall }, // flat, with its own prep wall
  battlefield: {
    ...slim('battlefield'), // its own size, flat, with its bases and points
    battle: (mapJson('battlefield') as { battle: unknown }).battle,
  },
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
  /** The veil's opacity: '1' down, '0' lifted. */
  veil(): string;
  /** Where label key stands on the screen, its bottom's y in pixels. */
  labelBottom(key: string): number;
  push: FakeWebSocket['push'];
}

const open: { world: World; socket: GameSocket; engine: NullEngine; left?: boolean }[] = [];
/** The chunks worlds asked for, each built here but those whose files failing names part of, as chunks/1_0.bin, how often a world closed its source, and how often one reloaded the page. */
const chunkWork = {
  builds: [] as Promise<unknown>[],
  closed: 0,
  failing: '',
  reloads: 0,
};
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
      chunks: {
        build: (req) => {
          const b =
            chunkWork.failing !== '' && req.url.includes(chunkWork.failing)
              ? Promise.reject(new Error('gone'))
              : buildChunk(req);
          chunkWork.builds.push(b);
          return b;
        },
        close: () => {
          chunkWork.closed++;
        },
      },
      now: () => t,
      reload: () => chunkWork.reloads++,
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
    veil: () =>
      [...parent.querySelectorAll<HTMLElement>('div')].find(
        (d) => d.style.background === 'rgb(10, 12, 22)',
      )!.style.opacity,
    labelBottom: (key) =>
      Number(
        /,\s*(-?[\d.]+)px\)$/.exec(
          parent.querySelector<HTMLElement>(`[data-label="${key}"]`)!.style.transform,
        )![1],
      ),
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

/** Back to real timers, any fake ones still due run first: Babylon queues its SetImmediate work behind one setTimeout, and a dropped one leaves the queue stuck for the file, holding every world after it. */
function realTimers(): void {
  if (vi.isFakeTimers()) {
    vi.runAllTimers();
  }
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
  return g?.weight === -1 ? 1 : (g?.weight ?? NaN); // Babylon's -1, unweighted, is all of it
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

/** Whether n stands near the map, not among the looks the warm plays far off it as the map starts (Fx.warm). */
function onMap(n: TransformNode): boolean {
  const v = n instanceof AbstractMesh ? n.getVerticesData(VertexBuffer.PositionKind) : null;
  const at = Vector3.TransformCoordinates(
    v ? Vector3.FromArray(v) : Vector3.Zero(),
    n.computeWorldMatrix(true),
  );
  return Math.abs(at.x) < NEAR_MAP_M && Math.abs(at.z) < NEAR_MAP_M;
}

/** p's meshes named name on its map. */
function meshesOn(p: Playing, name: string): AbstractMesh[] {
  return p.scene()!.meshes.filter((m) => m.name === name && onMap(m));
}

/** p's transform nodes named name on its map. */
function nodesOn(p: Playing, name: string): TransformNode[] {
  return p.scene()!.transformNodes.filter((n) => n.name === name && onMap(n));
}

/** Sends mobs for ticks from on, drawing a frame after each. */
function frames(p: Playing, from: number, to: number, mobs: MobWire[]): void {
  for (let tick = from; tick <= to; tick++) {
    p.push('delta', { mobs }, { tick });
    p.frames(1);
  }
}

/** Figures' own make, to call through from a spy on it. */
function unspied(): (this: Figures, look: ModelLook) => Promise<Figure> {
  return Object.getOwnPropertyDescriptor(Figures.prototype, 'make')!.value as (
    this: Figures,
    look: ModelLook,
  ) => Promise<Figure>;
}

/** A colour as a style reads it back. */
const rgb = (c: number) => `rgb(${(c >> 16) & 0xff}, ${(c >> 8) & 0xff}, ${c & 0xff})`;

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
    chunkWork.failing = '';
    chunkWork.builds.length = 0;
    vi.unstubAllGlobals();
    realTimers();
    vi.resetAllMocks();
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

  it('lights a region with cascaded shadows where the engine can cascade, and a map with its one shadow map', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const asked = vi.spyOn(CascadedShadowGenerator, 'IsSupported', 'get'); // the null engine can't: the one map's again
    await playing(FLAT, 10, 20);
    expect(asked).not.toHaveBeenCalled();
    await playing(REGIONED, 30, 60);
    expect(asked).toHaveBeenCalled();
    asked.mockRestore();
  });

  it('holds the keys, attacks and skills till the region’s ground round the player comes, saying so, and lifts the veil once the chunks they see are drawn', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(REGIONED, 30, 60);
    expect([connection.getState().groundLoading, p.veil()]).toEqual([true, '1']);
    key('ArrowRight', true);
    const from = p.inputs();
    p.frames(6);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.inputs()).toBeGreaterThan(from); // an input each tick still, every key let go
    expect(
      p
        .sent('input')
        .slice(from)
        .some((d) => d.right === true),
    ).toBe(false);
    expect(p.sent('attack')).toEqual([]);
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    p.frames(1);
    expect(connection.getState().groundLoading).toBe(false);
    expect(p.veil()).toBe('1'); // two chunks made a frame: half drawn
    p.frames(1);
    expect(p.veil()).toBe('0');
    p.frames(3);
    expect(p.sent('input').at(-1)?.right).toBe(true); // the key still down walks on
    key('ArrowRight', false);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1);
  });

  it('lets the ground’s notice go with a region left while its ground was coming', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(REGIONED, 30, 60);
    expect(connection.getState().groundLoading).toBe(true);
    changeMap(p, { mapId: FLAT, x: 4, y: 0, z: 20, yaw: 0 });
    expect(connection.getState().groundLoading).toBe(false);
  });

  it('holds the keys near a chunk that won’t come: within the farthest a skill carries a body, its radius, and the region’s widest solid', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    chunkWork.failing = 'chunks/1_0.bin';
    const held = async (x: number, z: number) => {
      const p = await playing(REGIONED, x, z);
      await Promise.allSettled(chunkWork.builds);
      await new Promise<void>((r) => setImmediate(r));
      p.frames(1);
      return connection.getState().groundLoading;
    };
    const reach = 10.5 + 0.375 + Math.hypot(3, 3.5); // Shadow Dance's blink, the body's radius, the 6 m by 7 m house
    expect(await held(64 - reach + 0.01, 30)).toBe(true); // chunk 1_0 starts at x 64
    expect(await held(64 - reach - 0.01, 30)).toBe(false);
  });

  it('reloads the page once, saying why, when a region’s chunks have failed every try', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    vi.spyOn(Math, 'random').mockReturnValue(0); // each wait at its floor, half its window: 0.25, 0.5, 1 and 2 s
    chunkWork.failing = 'chunks/1_'; // 1_0 and 1_1
    const p = await playing(REGIONED, 30, 60);
    vi.useFakeTimers({ toFake: ['setTimeout'] });
    const reloads = chunkWork.reloads;
    for (let i = 0; i < 5; i++) {
      await Promise.allSettled(chunkWork.builds);
      await new Promise<void>((r) => setImmediate(r));
      p.frames(i < 4 ? Math.ceil((250 * 2 ** i) / FRAME_MS) + 1 : 1); // past the wait, to the next try
    }
    expect(
      toasts.getState().toasts.filter((t) => t.text === 'The map’s ground didn’t load. Reloading…'),
    ).toHaveLength(1);
    vi.advanceTimersByTime(2900);
    expect(chunkWork.reloads).toBe(reloads); // the notice shows first
    vi.advanceTimersByTime(100);
    expect(chunkWork.reloads).toBe(reloads + 1);
  });

  // The World dropped a region's place with its scene, never the place's own dispose: a chunk whose last try failed after the player had gone still broke the region, and the page reloaded.
  it.each([
    ['leaves the game', (p: Playing) => leave(p)],
    [
      'goes to another map',
      (p: Playing) => changeMap(p, { mapId: FLAT, x: 4, y: 0, z: 20, yaw: 0 }),
    ],
  ])(
    'lets a region’s chunk work go as the player %s: a last try failing after reloads nothing',
    async (_, go) => {
      vi.spyOn(console, 'info').mockImplementation(() => undefined);
      vi.spyOn(Math, 'random').mockReturnValue(0); // each wait at its floor: 0.25, 0.5, 1 and 2 s
      chunkWork.failing = 'chunks/1_';
      const p = await playing(REGIONED, 30, 60);
      const reloads = chunkWork.reloads;
      for (let i = 0; i < 4; i++) {
        await Promise.allSettled(chunkWork.builds);
        await new Promise<void>((r) => setImmediate(r));
        p.frames(Math.ceil((250 * 2 ** i) / FRAME_MS) + 1); // past the wait: the fifth try is asked last
      }
      go(p); // with the fifth try in flight
      vi.useFakeTimers({ toFake: ['setTimeout'] });
      await Promise.allSettled(chunkWork.builds);
      await new Promise<void>((r) => setImmediate(r));
      vi.advanceTimersByTime(10_000);
      expect(
        toasts
          .getState()
          .toasts.filter((t) => t.text === 'The map’s ground didn’t load. Reloading…'),
      ).toEqual([]);
      expect(chunkWork.reloads).toBe(reloads);
    },
  );

  it('stands a region’s gate label on its ground once the chunk under it comes', async () => {
    const p = await playing(REGIONED, 86, 70);
    expect(p.label('portal:gate')).toBe(`To ${maps.find((m) => m.id === FLAT)!.name}`);
    const below = () => p.labelBottom('portal:gate') - p.labelBottom('me'); // the gate's 6 m off: the camera's easing moves both
    p.frames(3);
    const before = below(); // over ground not come, read flat at 0
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    p.frames(1);
    expect(below()).toBeGreaterThan(before + 10); // down on the river's bed, 2.4 m under the bank the player stands on
  });

  it.each([
    ['while the old map fades', true],
    ['once it has gone', false],
  ])(
    'takes a region’s channel from the Snapshot of the map it lands on, one coming %s too, never showing it with the old map',
    async (_, early) => {
      vi.spyOn(console, 'info').mockImplementation(() => undefined);
      const p = await playing(REGIONED, 30, 20);
      p.push('snapshot', { players: [standing(30, 20)], channel: 2 }, { tick: 2 });
      expect(mapView.getState()).toMatchObject({
        mapId: REGIONED,
        channel: 2,
        ground: { kind: 'tiles', folder: bakeUrl(TINY, 'tiles') }, // its bake's map tiles
      });
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
      p.push('mapChange', { mapId: REGIONED, x: 30, y: 0, z: 20, yaw: 0 }, { tick: 3 }); // a switch to channel 3
      const snapshot = () =>
        p.push('snapshot', { players: [standing(30, 20)], channel: 3 }, { tick: 4 });
      if (early) {
        snapshot();
        expect(mapView.getState().channel).toBe(2); // the old channel's map still shows
      }
      vi.runOnlyPendingTimers();
      vi.useRealTimers();
      if (!early) {
        snapshot();
      }
      await vi.waitFor(
        () => expect(mapView.getState()).toMatchObject({ mapId: REGIONED, channel: 3 }),
        WAIT,
      );
    },
  );

  it('names the region’s area a Snapshot lands the player in along the top, and holds an area entered while the region was building till it stands', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(FLAT, 4, 20);
    changeMap(p, { mapId: REGIONED, x: 30, y: 0, z: 20, yaw: 0 });
    p.push('snapshot', { players: [standing(30, 20)], area: 'field' }, { tick: 3 });
    p.push('areaEntered', { area: 'square' }, { tick: 4 });
    await vi.waitFor(() => expect(p.parent.textContent).toContain('Market Square'), WAIT);
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    p.frames(2);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toEqual([]); // the town's
    p.push('snapshot', { players: [standing(30, 20)], area: 'field' }, { tick: 5 }); // a rejoin
    expect(p.parent.textContent).toContain('Field · Lv 1–2');
  });

  it('names the region’s area the player steps into along the top, and in a town area, as a snapshot or a step names it, holds the lock, attacks and skills that harm, as in a town', async () => {
    vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(REGIONED, 30, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: REGIONED,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 1,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    const pack = [mob(1, 'green_slime', 33)];
    const press = (code: string) => {
      key(code, true);
      key(code, false);
    };
    const held = () => [target.getState().target, p.sent('attack'), p.sent('useSkill')];
    p.push('snapshot', { players: [standing(30, 20)], mobs: pack, area: 'square' }, { tick: 2 }); // a rejoin in the town
    frames(p, 3, 6, pack);
    press('KeyX');
    press('KeyQ');
    p.frames(2);
    expect(held()).toEqual([null, [], []]);
    p.push('areaEntered', { area: 'field' });
    frames(p, 7, 8, pack);
    expect(p.parent.textContent).toContain('Field · Lv 1–2');
    expect(target.getState().target?.ref).toBe('m:1');
    press('KeyX');
    expect(p.sent('attack')).toHaveLength(1);
    p.live(20); // the swing over
    p.push('areaEntered', { area: 'square' });
    frames(p, 29, 30, pack);
    expect(p.parent.textContent).toContain('Market Square');
    press('KeyX');
    press('KeyQ');
    p.frames(2);
    expect(held()).toEqual([null, [expect.anything()], []]);
  });

  it('lights a cave region by its torches: no sun and so no sun shadows, its ground sampling the light map', async () => {
    const p = await playing(CAVED, 24, 64);
    const scene = p.scene()!;
    expect(scene.lights).toEqual([]);
    p.frames(1);
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    p.frames(2);
    const chunks = scene.meshes.filter((m) => m.name.startsWith('chunk '));
    expect(chunks).toHaveLength(4);
    const map = (chunks[0]!.material as NodeMaterial).getBlockByName('lightMap') as TextureBlock;
    expect(map.texture?.getSize()).toEqual({ width: 128, height: 128 });
    expect(scene.clearColor.toHexString().slice(0, 7)).toBe('#000000');
  });

  it('draws a map whose data names a region from that bake, streaming its chunks round the camera', async () => {
    const p = await playing(REGIONED, 30, 60);
    const scene = p.scene()!;
    expect(scene.transformNodes.some((n) => n.name === `region ${TINY.id}`)).toBe(true);
    expect(scene.transformNodes.some((n) => n.name.startsWith('place '))).toBe(false);
    expect(scene.lights.map((l) => l.name)).toEqual(['sun']);
    p.frames(1);
    await Promise.allSettled(chunkWork.builds);
    await new Promise<void>((r) => setImmediate(r));
    p.frames(2); // two made a frame
    expect(scene.meshes.filter((m) => m.name.startsWith('chunk ')).length).toBe(4);
    expect(scene.getMeshByName('overview')!.getTotalIndices()).toBe(0); // every chunk drawn over it
    const closed = chunkWork.closed;
    changeMap(p, { mapId: FLAT, x: 4, y: 0, z: 20, yaw: 0 });
    expect(chunkWork.closed).toBe(closed + 1); // its workers end as it goes
    leave(p);
    expect(chunkWork.closed).toBe(closed + 2);
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

  it('Left Shift switches walk mode on its press alone, saying which; every input carries it, so the player never sprints, and switched back while walking sprints at once', async () => {
    const p = await playing(FLAT, 4, 20);
    const me = p.root(classLook('warrior').file)!;
    key('ShiftLeft', true);
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'ShiftLeft', repeat: true })); // held: no second switch
    key('ShiftLeft', false);
    expect(settings.getState().walk).toBe(true);
    expect(toasts.getState().toasts.map((t) => [t.text, t.kind])).toEqual([['Walk', 'info']]);
    const before = p.inputs();
    key('ArrowRight', true);
    p.live(50); // 2.5 s, past the sprint's 2 s
    const walkFrom = me.position.x;
    p.live(10);
    const walked = me.position.x - walkFrom;
    expect(
      p
        .sent('input')
        .slice(before)
        .every((d) => d.walk === true),
    ).toBe(true);
    const switched = p.inputs();
    key('ShiftLeft', true);
    key('ShiftLeft', false);
    expect(toasts.getState().toasts.at(-1)?.text).toBe('Sprint');
    p.live(1); // the switch's tick: its first sub-step walks
    const sprintFrom = me.position.x;
    p.live(10);
    key('ArrowRight', false);
    expect((me.position.x - sprintFrom) / walked).toBeCloseTo(movement.sprint.speed, 2);
    expect(
      p
        .sent('input')
        .slice(switched)
        .some((d) => d.walk === true),
    ).toBe(false);
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

  it('zooms the camera back and lower on the wheel and the zoom keys, easing there, the fog drawing back with it', async () => {
    const p = await playing(FLAT, 4, 20);
    const scene = p.scene()!;
    const camera = scene.activeCamera!;
    p.live(40);
    const rise = () => camera.position.y - CHEST_M; // the player stands on flat ground at 0
    const [start, end] = [scene.fogStart, scene.fogEnd];
    expect([rise(), start, camera.maxZ]).toEqual([
      expect.closeTo(16 * Math.sin((50 * Math.PI) / 180)),
      45, // the day sky's, close up
      1500,
    ]);
    const wheel = (deltaY: number, deltaMode: number = WheelEvent.DOM_DELTA_PIXEL) =>
      p.parent.dispatchEvent(new WheelEvent('wheel', { deltaY, deltaMode, cancelable: true }));
    expect(wheel(400)).toBe(false); // the page doesn't scroll
    expect(settings.getState().zoom).toBe(0.5);
    p.live(1);
    expect(rise()).toBeGreaterThan(16 * Math.sin((50 * Math.PI) / 180)); // on its way out
    expect(rise()).toBeLessThan(28 * Math.sin((42.5 * Math.PI) / 180));
    p.live(60);
    expect([rise(), scene.fogStart, scene.fogEnd]).toEqual([
      expect.closeTo(28 * Math.sin((42.5 * Math.PI) / 180)),
      expect.closeTo(start * 2.3),
      expect.closeTo(end * 2.3),
    ]);
    wheel(1, WheelEvent.DOM_DELTA_LINE); // a mouse that counts lines: a step a notch
    key('Minus', true);
    key('Minus', false);
    expect(settings.getState().zoom).toBe(0.75);
    for (let i = 0; i < 4; i++) {
      wheel(-100);
    }
    expect(settings.getState().zoom).toBe(0.25);
    for (let i = 0; i < 3; i++) {
      key('Equal', true);
      key('Equal', false);
    }
    expect(settings.getState().zoom).toBe(0); // held at its closest
    p.live(80);
    expect([rise(), scene.fogStart, scene.fogEnd]).toEqual([
      expect.closeTo(16 * Math.sin((50 * Math.PI) / 180)),
      start,
      end,
    ]);
    leave(p);
    wheel(400);
    expect(settings.getState().zoom).toBe(0); // the world gone, the wheel is the page's again
  });

  it('opens on the account’s zoom at once, its fog drawn back with it', async () => {
    settings.getState().setZoom(1);
    const p = await playing(FLAT, 40, 20);
    const scene = p.scene()!;
    expect([scene.fogStart, scene.fogEnd]).toEqual([
      expect.closeTo(45 * 3.6),
      expect.closeTo(140 * 3.6),
    ]);
    p.live(1);
    expect(scene.activeCamera!.position.y - CHEST_M).toBeCloseTo(
      40 * Math.sin((35 * Math.PI) / 180),
    );
  });

  it('tells the prediction the world’s fight from each Damage naming the player, ends the sprint’s count from the input after a sent attack or skill, and a rejoin is out of combat', async () => {
    const fought = vi.spyOn(Predictor.prototype, 'fought');
    const marked = vi.spyOn(Predictor.prototype, 'mark');
    const ended = vi.spyOn(Predictor.prototype, 'endFight');
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 1,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    p.live(2);
    p.push('damage', { source: 'm:1', target: 'p:c-robin', amount: 5 }, { tick: 30 }); // another's fight
    p.push('damage', { source: 'm:1', target: `p:${ME}`, amount: 5 }, { tick: 31 });
    p.push('damage', { source: `p:${ME}`, target: 'm:1', amount: 7 }, { tick: 32 });
    expect(fought.mock.calls).toEqual([
      [31, 100],
      [32, 100],
    ]); // 5 s of 50 ms ticks
    const pressed = p.socket.sequence;
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1);
    expect(marked.mock.calls).toEqual([[pressed, endSprint]]);
    p.live(20); // the swing over
    key('KeyQ', true);
    key('KeyQ', false);
    const skill = p.ws.frames().filter((f) => f.t === 'useSkill');
    expect(skill).toHaveLength(1);
    expect(marked.mock.calls.slice(1)).toEqual([[skill[0]!.seq, endSprint]]); // the input it went during
    expect(ended).not.toHaveBeenCalled();
    p.push('welcome', { token: 'tok-2', mapId: FLAT, tickRateHz: RATE }); // a rejoin on the same map
    expect(ended).toHaveBeenCalledOnce();
    for (const spy of [fought, marked, ended]) {
      spy.mockRestore();
    }
  });

  it('draws a body sprinting once its count reaches the sprint’s start: leaning forward, the player’s own and another’s', async () => {
    const p = await playing(FLAT, 4, 20);
    const lean = (file: string) =>
      p
        .root(file)
        ?.getChildren()
        .find((n) => n.name === `${file} lean`) as TransformNode | undefined;
    const robin = (tick: number, sprint: number): PlayerWire => ({
      ...standing(8 + tick * 0.4, 32, 'c-robin', 'thief'),
      vx: 8,
      sprint,
    });
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', { players: [robin(tick, 117)] }, { tick });
      p.frames(3);
    }
    await vi.waitFor(() => expect(lean(classLook('thief').file)).toBeDefined(), WAIT);
    p.frames(10);
    expect(lean(classLook('thief').file)!.rotation.x).toBe(0); // 8 m/s, its count short: a run
    for (let tick = 9; tick <= 16; tick++) {
      p.push('delta', { players: [robin(tick, 120)] }, { tick });
      p.frames(3);
    }
    expect(lean(classLook('thief').file)!.rotation.x).toBeGreaterThan(0.1);
    const me = lean(classLook('warrior').file)!;
    key('ArrowRight', true);
    p.live(4);
    expect(me.rotation.x).toBe(0);
    const acked = p.socket.sequence - 2;
    p.push(
      'delta',
      { players: [{ ...standing(5, 20), vx: 8, hx: 1, sprint: 120 }], ackSeq: acked },
      { tick: 17 },
    ); // the world has them 2 s into a walk
    p.live(10);
    key('ArrowRight', false);
    expect(me.rotation.x).toBeGreaterThan(0.1);
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

  it('prompts the talk key, as bound, over the NPC in reach or the portal the player stands in, and nowhere else', async () => {
    const near = await playing('town', 16, 14.5, 'thief');
    near.frames(1);
    expect(near.label('prompt')).toBe('Z Talk');
    settings.getState().setKeys({ talk: 'KeyG' });
    near.frames(1);
    expect(near.label('prompt')).toBe('G Talk');
    quests.getState().applyNpcDialog(create(NpcDialogSchema, { npcId: 'town_guide' }));
    ui.getState().openWindow('npc');
    near.frames(1);
    expect(near.label('prompt')).toBe(''); // its dialog is open
    leave(near);

    settings.getState().setKeys({});
    const inPortal = await playing('town', 46, 16, 'thief');
    inPortal.frames(1);
    expect(inPortal.label('prompt')).toBe('Z Enter');
    leave(inPortal);

    const away = await playing(FLAT, 4, 20);
    away.frames(1);
    expect(away.label('prompt')).toBe('');
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

  it.each([
    [
      'dies, then is removed',
      [
        { tick: 9, dead: true },
        { tick: 12, removed: true },
      ],
    ],
    ['dies and is removed in one frame', [{ tick: 9, dead: true, removed: true }]],
    [
      'dies, then rises in the frame that removes it',
      [
        { tick: 9, dead: true },
        { tick: 12, dead: false, removed: true },
      ],
    ],
  ] as const)(
    'keeps a player who %s down until its figure goes, never standing it up first',
    async (_, steps: readonly { tick: number; dead?: boolean; removed?: boolean }[]) => {
      const p = await playing(FLAT, 4, 20);
      for (let tick = 2; tick <= 8; tick++) {
        p.push('delta', tick === 2 ? { players: [standing(8, 20, 'c-robin', 'thief')] } : {}, {
          tick,
        });
        p.frames(1);
      }
      await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
      const posed = vi.spyOn(Body.prototype, 'pose');
      try {
        for (let tick = 9; tick <= 18; tick++) {
          const step = steps.find((s) => s.tick === tick);
          const robin =
            step?.dead === undefined
              ? []
              : [
                  {
                    ...standing(8, 20, 'c-robin', 'thief'),
                    hp: step.dead ? 0 : 50,
                    dead: step.dead,
                  },
                ];
          p.push(
            'delta',
            { players: robin, ...(step?.removed && { removedPlayers: ['c-robin'] }) },
            { tick },
          );
          p.frames(1);
        }
        const robins = posed.mock.calls
          .filter((_, i) => (posed.mock.contexts[i] as Body).root.name === classLook('thief').file)
          .map(([pose]) => pose);
        expect(p.root(classLook('thief').file)).toBeUndefined();
        expect(robins).toContain('dead');
        expect(robins.slice(robins.indexOf('dead'))).not.toContain('up');
      } finally {
        posed.mockRestore();
      }
    },
  );

  it('takes away a player gone out of sight as one who left', async () => {
    const p = await playing(FLAT, 4, 20);
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', tick === 2 ? { players: [standing(8, 20, 'c-robin', 'thief')] } : {}, {
        tick,
      });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
    for (let tick = 9; tick <= 14; tick++) {
      p.push('delta', tick === 9 ? { unseen: { players: ['c-robin'] } } : {}, { tick });
      p.frames(1);
    }
    expect(p.root(classLook('thief').file)).toBeUndefined();
    expect(nearby.getState().players).toEqual([]);
  });

  describe('the side edge every frame brings', () => {
    let plays: { id: string; on: () => Unit | undefined; stopped: boolean }[] = [];
    let spy: { mockRestore(): void } | undefined;
    beforeEach(() => {
      plays = [];
      spy = vi.spyOn(Fx.prototype, 'play').mockImplementation((id, o) => {
        const m = { id, on: () => o.follow?.(), stopped: false };
        plays.push(m);
        return () => {
          m.stopped = true;
        };
      });
    });
    afterEach(() => {
      spy?.mockRestore();
    });

    const across = async (mapId: string, jadeX: number) => {
      const p = await playing(mapId, 4, 20);
      const [kai, robin, jade] = ['magician', 'thief', 'bowman'].map((c) => classLook(c).file) as [
        string,
        string,
        string,
      ];
      const slime = mobLook('green_slime').file;
      let tick = 1;
      const slow = [create(StatusViewSchema, { id: 'slow', ends: 9999 })];
      const boar = { ...mob(2, 'wild_boar', 30), statuses: slow };
      let boarSent = true;
      const frame = (x: number, boarGone = false) => {
        const leaving = boarGone && boarSent; // said once, as the server does
        boarSent = !boarGone;
        const players = [
          standing(60, 20, 'c-kai', 'magician'),
          { ...standing(94, 20, 'c-robin', 'thief'), statuses: slow, shield: 10 },
          { ...standing(x, 20, 'c-jade', 'bowman'), statuses: slow, shield: 10 },
        ];
        const mobs = [
          { ...mob(1, 'green_slime', x), hp: 34 - (tick % 30) }, // hurt each frame, so it shows its bar while drawn
          ...(boarGone ? [] : [boar]),
        ];
        p.push(
          'delta',
          { players, mobs, ...(leaving && { unseen: { mobs: [2] } }) },
          { tick: ++tick },
        );
        p.frames(1);
      };
      frame(jadeX);
      await vi.waitFor(
        () => expect([kai, robin, jade, slime].every((f) => p.root(f))).toBe(true),
        WAIT,
      );
      frame(jadeX);
      frame(jadeX);
      const look = (file: string) => {
        const root = p.root(file); // enabled ones only: each was in before the frames that hid it
        const faded = root
          ?.getChildMeshes(false)
          .some((m) => m.material?.name.endsWith(` ${FADED}`));
        return root ? (faded ? 'faded' : 'whole') : 'hidden';
      };
      const seen = () => ({
        kai: look(kai),
        robin: look(robin),
        jade: [look(jade), p.label('p:c-jade') ?? null],
        slime: [look(slime), p.parent.querySelector('[data-bar="1"]') !== null],
        marked: plays
          .filter((m) => m.id === 'status.slow' && !m.stopped)
          .map((m) => Math.round(m.on()?.x ?? NaN))
          .sort((a, b) => a - b),
        bubbles: p
          .scene()!
          .meshes.filter((m) => m.name === 'shield bubble' && m.isEnabled())
          .map((m) => Math.round(m.position.x))
          .sort((a, b) => a - b),
      });
      return { frame, seen };
    };

    it('fades a body toward it on a map cut by cells, draws none past it, with no name or bar, and draws one back as it comes within', async () => {
      const { frame, seen } = await across(REGIONED, 104); // the player at 4: Kai 56 m across, Robin 90, Jade 100
      expect(seen()).toEqual({
        kai: 'whole',
        robin: 'faded',
        jade: ['hidden', null],
        slime: ['hidden', false],
        marked: [30, 94],
        bubbles: [94],
      });
      for (let i = 0; i < 5; i++) {
        frame(104, true);
      }
      expect(seen().marked).toEqual([94]); // the boar went out of sight while Jade was hidden
      frame(99);
      frame(99);
      expect(seen()).toEqual({
        kai: 'whole',
        robin: 'faded',
        jade: ['faded', 'Robin'],
        slime: ['faded', true],
        marked: [30, 94, 99],
        bubbles: [94, 99],
      });
    });

    it('draws every body whole on a map sent whole, however far across', async () => {
      const { seen } = await across(WIDE, 124);
      expect(seen()).toEqual({
        kai: 'whole',
        robin: 'whole',
        jade: ['whole', 'Robin'],
        slime: ['whole', true],
        marked: [30, 94, 124],
        bubbles: [94, 124],
      });
    });
  });

  it('redraws a player whose class changes, keeping the old figure till the new one is in, and the newest change winning', async () => {
    const p = await playing(FLAT, 4, 20);
    const [thief, bowman, mage] = ['thief', 'bowman', 'magician'].map((c) => classLook(c).file);
    for (let tick = 2; tick <= 6; tick++) {
      p.push('delta', tick === 2 ? { players: [standing(8, 20, 'c-robin', 'thief')] } : {}, {
        tick,
      });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(thief!)).toBeDefined(), WAIT);
    const real = unspied();
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    let bowmanBuilt: Promise<unknown> = Promise.resolve();
    const make = vi.spyOn(Figures.prototype, 'make').mockImplementation(function (
      this: Figures,
      look,
    ) {
      const made = real.call(this, look);
      if (look.file === bowman) {
        bowmanBuilt = made;
      }
      return look.file === bowman ? held.then(() => made) : made;
    });
    p.push('delta', { players: [standing(8, 20, 'c-robin', 'bowman')] }, { tick: 7 });
    p.frames(3);
    await bowmanBuilt;
    await new Promise((r) => setImmediate(r));
    expect(p.root(thief!)).toBeDefined(); // the bowman built but held back
    expect(make).toHaveBeenCalledTimes(1);
    p.push('delta', { players: [standing(8, 20, 'c-robin', 'magician')] }, { tick: 8 });
    p.frames(1);
    await vi.waitFor(() => expect(p.root(mage!)).toBeDefined(), WAIT);
    expect(p.root(thief!)).toBeUndefined();
    release();
    await Promise.allSettled(make.mock.results.map((r) => r.value as Promise<unknown>));
    await new Promise((r) => setTimeout(r, 0));
    expect(p.root(bowman!)).toBeUndefined(); // come after the magician, so dropped
    expect(p.root(mage!)).toBeDefined();
    p.frames(1);
    expect(p.root(mage!)!.position.x).toBeCloseTo(8);
  });

  it('dresses another player in what their look wears, and redraws them when it changes', async () => {
    const p = await playing(FLAT, 4, 20);
    const shown = (name: string) =>
      !!p.scene()?.meshes.some((m) => m.name === name && m.isEnabled());
    const robin = (look: Partial<LookWire>) => ({
      ...standing(8, 20, 'c-robin', 'thief'),
      look: create(LookSchema, look),
    });
    for (let tick = 2; tick <= 6; tick++) {
      p.push('delta', tick === 2 ? { players: [robin({ outfit: 'mage_robe' })] } : {}, { tick });
      p.frames(1);
    }
    await vi.waitFor(() => expect(shown('Mage_Body worn')).toBe(true), WAIT);
    expect(shown('Rogue_Body')).toBe(false);
    p.push(
      'delta',
      { players: [robin({ outfit: 'knight_plate', weapon: 'greataxe' })] },
      { tick: 7 },
    );
    p.frames(1);
    await vi.waitFor(() => expect(shown('Knight_Body worn')).toBe(true), WAIT);
    expect(shown('2H_Axe worn')).toBe(true);
    expect(shown('Mage_Body worn')).toBe(false);
    expect(shown('Knight_Body')).toBe(true); // the player's own knight, as their class draws them
  });

  it('draws a player of a class and a mob of a type this client’s data lacks as stand-ins, and keeps drawing the world', async () => {
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(FLAT, 4, 20);
    for (let tick = 2; tick <= 6; tick++) {
      p.push(
        'delta',
        tick === 2
          ? { players: [standing(8, 20, 'c-robin', 'pirate')], mobs: [mob(1, 'dragon', 12)] }
          : {},
        { tick },
      );
      p.frames(1);
    }
    const smallest = [...mobData].sort((a, b) => a.radius - b.radius)[0]!;
    await vi.waitFor(() => expect(p.root(classLook('beginner').file)).toBeDefined(), WAIT);
    await vi.waitFor(() => expect(p.root(mobLook(smallest.id).file)).toBeDefined(), WAIT);
    const before = p.inputs();
    p.live(5);
    expect(p.inputs() - before).toBe(5); // still stepping
    expect(said).toHaveBeenCalledWith(expect.stringContaining('no class pirate'));
    expect(said).toHaveBeenCalledWith(expect.stringContaining('no mob dragon'));
    said.mockRestore();
  });

  it('draws a player wearing a piece this client’s data lacks in their class’s own for it, and keeps drawing the world', async () => {
    const said = vi.spyOn(console, 'info').mockImplementation(() => undefined);
    const p = await playing(FLAT, 4, 20);
    const shown = (name: string) =>
      !!p.scene()?.meshes.some((m) => m.name === name && m.isEnabled());
    for (let tick = 2; tick <= 6; tick++) {
      const look = create(LookSchema, { outfit: 'mage_robe', headwear: 'crown_of_tomorrow' });
      p.push(
        'delta',
        tick === 2 ? { players: [{ ...standing(8, 20, 'c-robin', 'thief'), look }] } : {},
        {
          tick,
        },
      );
      p.frames(1);
    }
    await vi.waitFor(() => expect(shown('Mage_Body worn')).toBe(true), WAIT);
    const before = p.inputs();
    p.live(5);
    expect(p.inputs() - before).toBe(5); // still stepping
    expect(said).toHaveBeenCalledWith(expect.stringContaining('no piece crown_of_tomorrow'));
    said.mockRestore();
  });

  it('dresses the player in what their own frame says they wear, and keeps the figure through a look that wears nothing', async () => {
    const make = vi.spyOn(Figures.prototype, 'make');
    const p = await playing(FLAT, 4, 20);
    const shown = (name: string) =>
      !!p.scene()?.meshes.some((m) => m.name === name && m.isEnabled());
    const made = make.mock.calls.length;
    p.push('delta', { players: [{ ...standing(4, 20), look: create(LookSchema) }] }, { tick: 2 });
    p.frames(2);
    expect(make.mock.calls.length).toBe(made);
    p.push(
      'delta',
      { players: [{ ...standing(4, 20), look: create(LookSchema, { headwear: 'pointed_hat' }) }] },
      { tick: 3 },
    );
    p.frames(1);
    await vi.waitFor(() => expect(shown('Mage_Hat worn')).toBe(true), WAIT);
    expect(shown('Knight_Helmet')).toBe(false);
    expect(make.mock.calls.length).toBe(made + 1);
  });

  it('dresses the player from a Snapshot too, as one rejoining in a look gets', async () => {
    const p = await playing(FLAT, 4, 20);
    const shown = (name: string) =>
      !!p.scene()?.meshes.some((m) => m.name === name && m.isEnabled());
    p.push(
      'snapshot',
      { players: [{ ...standing(4, 20), look: create(LookSchema, { weapon: 'greataxe' }) }] },
      { tick: 2 },
    );
    p.frames(1);
    await vi.waitFor(() => expect(shown('2H_Axe worn')).toBe(true), WAIT);
  });

  it('poses another player every frame near the camera’s target, every 2nd frame 45 m off, and never behind the camera', async () => {
    const update = vi.spyOn(Body.prototype, 'update');
    settings.getState().setZoom(1); // at its farthest, where ground 45 m off is in view
    const p = await playing(FLAT, 4, 20);
    const others = [
      standing(6, 20, 'c-near', 'thief'),
      standing(4, 65, 'c-far', 'thief'),
      standing(4, -40, 'c-behind', 'thief'),
    ];
    for (let tick = 2; tick <= 6; tick++) {
      p.push('delta', tick === 2 ? { players: others } : {}, { tick });
      p.frames(1);
    }
    const at = (x: number, z: number) =>
      update.mock.calls.flatMap((c, i) => {
        const root = (update.mock.contexts[i] as Body).root.position;
        return root.x === x && root.z === z ? [c[3]] : [];
      });
    await vi.waitFor(() => {
      p.frames(1);
      expect(at(4, -40).length * at(4, 65).length).toBeGreaterThan(0); // every figure in
    }, WAIT);
    update.mockClear();
    p.frames(8);
    expect(at(6, 20)).toEqual(Array(8).fill(true));
    const alternate = [true, false, true, false, true, false, true, false];
    expect([alternate, alternate.map((b) => !b)]).toContainEqual(at(4, 65));
    expect(at(4, -40)).toEqual(Array(8).fill(false));
    expect(at(4, 20)).toEqual(Array(8).fill(true)); // the player, the camera's target
  });

  it('poses another player whose middle is just off the screen’s side while their body reaches onto it', async () => {
    const update = vi.spyOn(Body.prototype, 'update');
    const p = await playing(FLAT, 4, 20);
    p.frames(2);
    const planes = Frustum.GetPlanes(p.scene()!.getTransformMatrix());
    const middle = classLook('thief').height / 2;
    const off = (x: number) =>
      -Math.min(...planes.map((q) => q.signedDistanceTo(new Vector3(x, middle, 20))));
    let [lo, hi] = [4, 60]; // the screen's east edge lies between, where a middle 1 m off is
    while (hi - lo > 0.001) {
      const x = (lo + hi) / 2;
      [lo, hi] = off(x) < 1 ? [x, hi] : [lo, x];
    }
    p.push('delta', { players: [standing(lo, 20, 'c-edge', 'thief')] }, { tick: 2 });
    const at = () =>
      update.mock.calls.flatMap((c, i) =>
        (update.mock.contexts[i] as Body).root.position.x === lo ? [c[3]] : [],
      );
    await vi.waitFor(() => {
      p.frames(1);
      expect(at().length).toBeGreaterThan(0);
    }, WAIT);
    update.mockClear();
    p.frames(4);
    expect([off(lo), at()]).toEqual([expect.closeTo(1, 2), [true, true, true, true]]);
  });

  it('poses the player every frame, even far from the camera’s target, as after a respawn across the map', async () => {
    const update = vi.spyOn(Body.prototype, 'update');
    const p = await playing(FLAT, 4, 20);
    p.frames(2);
    p.push('died', { characterId: ME, x: 4, y: 0, z: 20 }, { tick: 2 });
    p.push('delta', { players: [standing(68, 20)], ackSeq: 0 }, { tick: 3 });
    p.push('respawned', { characterId: ME, mapId: FLAT, x: 68, y: 0, z: 20, hp: 50 }, { tick: 3 });
    update.mockClear();
    p.frames(8);
    const mine = update.mock.calls.flatMap((c, i) =>
      (update.mock.contexts[i] as Body).root.position.x === 68 ? [c[3]] : [],
    );
    expect(mine).toEqual(Array(8).fill(true)); // 64 m from where the camera eases from, every 4th frame were it paced
  });

  it('poses the locked mob every frame, even 40 m off', async () => {
    const update = vi.spyOn(Body.prototype, 'update');
    settings.getState().setZoom(1); // at its farthest, where it's in view to lock
    const p = await playing(FLAT, 4, 20);
    const far = { ...mob(1, 'green_slime', 4), z: 60 };
    frames(p, 2, 6, [far]);
    await vi.waitFor(() => {
      frames(p, 7, 7, [far]);
      expect(outlined(p, 'quaternius/GreenBlob.glb')).toBe(true);
    }, WAIT);
    update.mockClear();
    frames(p, 8, 15, [far]);
    const slime = update.mock.calls.flatMap((c, i) =>
      (update.mock.contexts[i] as Body).root.position.z === 60 ? [c[3]] : [],
    );
    expect(slime).toEqual(Array(8).fill(true));
  });

  it('poses a locked enemy every frame in a match, even 40 m off', async () => {
    const update = vi.spyOn(Body.prototype, 'update');
    settings.getState().setZoom(1);
    const p = await playing(FLAT, 4, 20);
    const players = [
      { ...standing(4, 20), team: 1 },
      { ...standing(4, 60, 'c-robin', 'thief'), team: 2 },
    ];
    let tick = 2;
    await vi.waitFor(() => {
      p.push('delta', { players }, { tick: tick++ });
      p.frames(1);
      expect(target.getState().target).not.toBeNull();
      expect(p.root(classLook('thief').file)).toBeDefined();
    }, WAIT);
    update.mockClear();
    for (let k = 0; k < 8; k++) {
      p.push('delta', { players }, { tick: tick++ });
      p.frames(1);
    }
    const robin = update.mock.calls.flatMap((c, i) =>
      (update.mock.contexts[i] as Body).root.position.z === 60 ? [c[3]] : [],
    );
    expect(robin).toEqual(Array(8).fill(true));
  });

  it('keeps another player’s shadow on, or off, within a metre of 25 m, so one on the line doesn’t flicker', async () => {
    const p = await playing(FLAT, 4, 20);
    const casting = (z: number) => {
      const list = p.scene()!.lights[0]!.getShadowGenerator()!.getShadowMap()!.renderList!;
      const root = p
        .scene()!
        .transformNodes.find((n) => n.name === classLook('thief').file && n.position.z === z);
      return !!root && root.getChildMeshes(false).some((m) => list.includes(m));
    };
    const at = (east: number, north: number, tick: number) => {
      p.push(
        'delta',
        {
          players: [
            standing(4 + east, 20, 'c-east', 'thief'),
            standing(4, 20 + north, 'c-north', 'thief'),
          ],
        },
        { tick },
      );
      p.live(16);
    };
    at(24, 27, 2);
    await vi.waitFor(() => {
      p.frames(1);
      expect([casting(20), casting(47)]).toEqual([true, false]);
    }, WAIT);
    at(25.5, 25.5, 30);
    expect([casting(20), casting(45.5)]).toEqual([true, false]); // both on the line, each as it was
    at(26.5, 23.5, 60);
    expect([casting(20), casting(43.5)]).toEqual([false, true]);
  });

  it('never lets a figure past 25 m cast, not even in a frame drawn before its player is, when it’s made or redrawn', async () => {
    const p = await playing(FLAT, 4, 20);
    const shadowMap = p.scene()!.lights[0]!.getShadowGenerator()!.getShadowMap()!;
    let cast = 0;
    shadowMap.onBeforeRenderObservable.add(() => {
      const thieves = p.scene()!.transformNodes.filter((n) => n.name === classLook('thief').file); // a new figure stands at the origin till it's drawn
      cast += thieves.some((n) =>
        n.getChildMeshes(false).some((m) => shadowMap.renderList!.includes(m)),
      )
        ? 1
        : 0;
    });
    const robin = (look: Partial<LookWire>) => ({
      ...standing(34, 20, 'c-robin', 'thief'),
      look: create(LookSchema, look),
    });
    const arrived = async (meshName: string) =>
      vi.waitFor(() => {
        p.scene()!.render(); // as a frame does that the remotes' loop skips
        expect(p.scene()!.meshes.some((m) => m.name === meshName && m.isEnabled())).toBe(true);
      }, WAIT);
    p.push('delta', { players: [robin({})] }, { tick: 2 });
    p.frames(1);
    await arrived('Rogue_Body');
    p.push('delta', { players: [robin({ headwear: 'pointed_hat' })] }, { tick: 3 });
    p.frames(1);
    await arrived('Mage_Hat worn');
    p.frames(2);
    expect(cast).toBe(0); // 30 m off
  });

  it('lets another player cast a shadow within 25 m of the camera’s target, and none past it', async () => {
    const p = await playing(FLAT, 4, 20);
    const casting = (x: number) => {
      const root = p
        .scene()!
        .transformNodes.find((n) => n.name === classLook('thief').file && n.position.x === x);
      const list = p.scene()!.lights[0]!.getShadowGenerator()!.getShadowMap()!.renderList!;
      return !!root && root.getChildMeshes(false).some((m) => list.includes(m));
    };
    for (let tick = 2; tick <= 6; tick++) {
      p.push(
        'delta',
        tick === 2
          ? { players: [standing(28, 20, 'c-near', 'thief'), standing(30, 20, 'c-far', 'thief')] }
          : {},
        { tick },
      );
      p.frames(1);
    }
    await vi.waitFor(() => {
      p.frames(1);
      expect([casting(28), casting(30)]).toEqual([true, false]); // 24 m and 26 m off
    }, WAIT);
    p.push(
      'delta',
      { players: [standing(31, 20, 'c-near', 'thief'), standing(20, 20, 'c-far', 'thief')] },
      { tick: 7 },
    );
    p.live(30);
    expect([casting(31), casting(20)]).toEqual([false, true]); // walked out and in
  });

  it('redraws the player on a promotion, keeping the old figure till the new one is in', async () => {
    const p = await playing(FLAT, 4, 20);
    const real = unspied();
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    vi.spyOn(Figures.prototype, 'make').mockImplementation(function (this: Figures, look) {
      return held.then(() => real.call(this, look));
    });
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: { id: ME, name: 'Ayla', class: 'thief', level: 30, hp: 50, maxHp: 50 },
      }),
    );
    p.frames(2);
    expect(p.root(classLook('warrior').file)).toBeDefined();
    release();
    await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
    expect(p.root(classLook('warrior').file)).toBeUndefined();
  });

  it('names the 40 other players nearest, a party member and the lock however far, and no one else', async () => {
    const p = await playing(FLAT, 4, 20);
    const near = Array.from({ length: 40 }, (_, i) => ({
      ...standing(2 + (i % 10) / 2, 19 + Math.floor(i / 10) / 2, `c-${i}`),
      name: `P${i}`,
      team: 1,
    })); // within 2.7 m
    const robin = { ...standing(4, 25, 'c-robin', 'thief'), team: 2 }; // 5 m north: the enemy, locked
    const jade = { ...standing(10, 24, 'c-jade', 'bowman'), name: 'Jade', team: 1 }; // the party's, 7.2 m
    const lee = { ...standing(7, 22, 'c-lee', 'bowman'), name: 'Lee', team: 1 }; // 3.6 m, the 41st nearest, and no one's
    party.getState().applyPartyState(
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: ME,
          members: [
            { characterId: ME, name: 'Ayla', mapId: FLAT, online: true },
            { characterId: 'c-jade', name: 'Jade', mapId: FLAT, online: true },
          ],
        }),
      }),
    );
    for (let tick = 2; tick <= 10; tick++) {
      p.push(
        'delta',
        { players: [{ ...standing(4, 20), team: 1 }, ...near, robin, jade, lee] },
        { tick },
      );
      p.frames(1);
    }
    expect(target.getState().target?.ref).toBe('p:c-robin');
    expect(near.every((n) => p.label(`p:${n.id}`) === n.name)).toBe(true);
    expect([p.label('p:c-robin'), p.label('p:c-jade'), p.label('p:c-lee')]).toEqual([
      'Robin',
      'Jade',
      undefined,
    ]);
  });

  it('in a match names enemies red and teammates green, tags a bot, tags a fighter whose place is held Reconnecting, and locks and attacks an enemy as a mob, never a teammate', async () => {
    const cut = vi.spyOn(Palette.prototype, 'cutAround');
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 30,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1, ground_slam: 1 } } },
        },
      }),
    );
    skillBar.getState().load(['strike']);
    const robin = { ...standing(8, 20, 'c-robin', 'thief'), level: 30 };
    const stun = () => create(StatusViewSchema, { id: 'stun', ends: 99 });
    const kai = { ...standing(4, 22, 'c-kai', 'bowman'), name: 'Kai', level: 30 };
    const name = (id: string) => p.parent.querySelector<HTMLElement>(`[data-label="p:${id}"]`)!;
    const tag = (id: string) => name(id).querySelector('span')!;
    const players = (
      team: number,
      foe: { hp?: number; dead?: boolean; statuses?: ReturnType<typeof stun>[] } = {},
    ): PlayerWire[] => [
      { ...standing(4, 20), team },
      { ...robin, team: team && 2, bot: team > 0, ...foe },
      { ...kai, team },
    ];
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', { players: players(0) }, { tick });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(classLook('bowman').file)).toBeDefined(), WAIT);
    p.push('delta', { players: players(0) }, { tick: 9 });
    p.frames(1);
    expect([name('c-robin').style.color, tag('c-robin').hidden]).toEqual([rgb(0xf4f4f8), true]);
    expect(target.getState().target).toBeNull(); // outside a match no player is an enemy
    for (let tick = 10; tick <= 12; tick++) {
      p.push('delta', { players: players(1) }, { tick });
      p.frames(1);
    }
    expect([
      name('c-robin').style.color,
      name('c-robin').firstChild?.textContent,
      tag('c-robin').textContent,
      tag('c-robin').hidden,
    ]).toEqual([rgb(0xff6b62), 'Robin', 'BOT', false]);
    expect([name('c-kai').style.color, tag('c-kai').hidden]).toEqual([rgb(0x7fe08a), true]);
    expect(target.getState().target).toEqual({
      kind: 'player',
      ref: 'p:c-robin',
      name: 'Robin',
      cls: 'thief',
      level: 30,
      hp: 50,
      maxHp: 50,
      bot: true,
    });
    const ring = p.scene()!.getMeshByName('lock ring')!;
    expect([ring.isEnabled(), ring.position.x, ring.position.z]).toEqual([true, 8, 20]);
    key('Tab', true);
    key('Tab', false);
    p.push('delta', { players: players(1) }, { tick: 13 });
    p.frames(1);
    expect(target.getState().target?.ref).toBe('p:c-robin'); // the teammate nearer is never locked
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack').map((d) => d.target)).toEqual(['p:c-robin']);
    expect(skillBar.getState().live[0]).toBe('strike');
    p.push('delta', { players: players(1, { statuses: [stun()] }) }, { tick: 14 });
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('ground_slam'); // a stunned enemy offers it, as a stunned mob does
    expect(statuses.getState().target.map((t) => t.id)).toEqual(['stun']);
    for (let tick = 15; tick <= 19; tick++) {
      p.push('delta', { players: players(1, { hp: 0, dead: true }) }, { tick });
      p.frames(1);
    }
    expect(target.getState().target).toBeNull(); // down, so no longer lockable
    p.push('died', { characterId: ME, x: 4, y: 0, z: 20 }, { tick: 20 });
    for (let tick = 21; tick <= 23; tick++) {
      p.push('delta', { players: players(1).slice(1) }, { tick }); // the player's own frame unchanged while down
      p.frames(1);
    }
    expect([name('c-robin').style.color, name('c-kai').style.color]).toEqual([
      rgb(0xff6b62),
      rgb(0x7fe08a),
    ]); // still an enemy and a teammate while the player is down
    expect(cut.mock.lastCall?.[3].map((o) => [Math.round(o.x), Math.round(o.z)])).toEqual([
      [8, 20],
    ]); // the place opens round the enemy 4 m off, not the teammate
    p.push('delta', { players: players(1) }, { tick: 24 });
    p.push('respawned', { characterId: ME, mapId: FLAT, x: 4, y: 0, z: 20, hp: 50 }, { tick: 24 });
    for (let tick = 25; tick <= 27; tick++) {
      p.push('delta', {}, { tick });
      p.frames(1);
    }
    expect(target.getState().target?.ref).toBe('p:c-robin'); // the round's reset: up, and locked on again
    const held = (away: boolean) =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'team',
          phase: MatchPhase.FIGHT,
          sides: [
            { fighters: [{ id: ME }, { id: 'c-kai', name: 'Kai', away }] },
            { fighters: [{ id: 'c-robin', name: 'Robin', bot: true }] },
          ],
        }),
        28,
      );
    held(true);
    p.push('delta', { players: players(1) }, { tick: 28 });
    p.frames(1);
    expect([tag('c-kai').textContent, tag('c-kai').hidden, tag('c-robin').textContent]).toEqual([
      'Reconnecting',
      false,
      'BOT',
    ]);
    held(false);
    p.push('delta', { players: players(1) }, { tick: 29 });
    p.frames(1);
    expect(tag('c-kai').hidden).toBe(true); // back
    cut.mockRestore();
  });

  it('a round’s reset gets up the one still standing too: their dodge and swing end, as a death’s do', async () => {
    const p = await playing(FLAT, 10, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 30,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { roll_back: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['roll_back']);
    const faded = () =>
      p
        .root(classLook('warrior').file)!
        .getChildMeshes(false)
        .some((m) => m.material?.name.endsWith(` ${FADED}`));
    p.live(2);
    key('KeyQ', true);
    key('KeyQ', false);
    p.live(1);
    expect([p.sent('useSkill').length, faded()]).toEqual([1, true]);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toEqual([]); // Roll Back still runs
    p.push('delta', { players: [standing(10, 20)], ackSeq: 0 }, { tick: 20 });
    p.push('respawned', { characterId: ME, mapId: FLAT, x: 10, y: 0, z: 20, hp: 50 }, { tick: 20 });
    p.frames(1);
    expect(faded()).toBe(false);
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1); // nothing left running
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

    it('go out of sight as they are, with no death, gone once drawn past', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 8)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      p.push('delta', { unseen: { mobs: [1] } }, { tick: 11 });
      p.frames(1);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Death')).not.toBe(1);
      p.frames(20);
      expect(p.root('quaternius/GreenBlob.glb')).toBeUndefined();
    });

    it('feed the boss bar the field boss the frames send from anywhere on the map, never drawing it from them; drawn, it shows the drawn one', async () => {
      const make = vi.spyOn(Figures.prototype, 'make');
      const p = await playing(FLAT, 4, 20);
      fieldBoss
        .getState()
        .applyFieldBoss(create(FieldBossSchema, { mobId: 9, type: 'king_slime' }));
      const king = { ...mob(9, 'king_slime', 6), hp: 700, maxHp: 1000 };
      for (let tick = 2; tick <= 6; tick++) {
        p.push('delta', { boss: king }, { tick });
        p.frames(1);
      }
      expect(fieldBoss.getState().hp).toEqual({ hp: 700, maxHp: 1000 });
      expect(make.mock.calls.some(([look]) => look.file === 'quaternius/GreenBlob_Crown.glb')).toBe(
        false,
      );
      for (let tick = 7; tick <= 12; tick++) {
        p.push('delta', { mobs: [{ ...king, hp: 650 }], boss: { ...king, hp: 640 } }, { tick }); // drawn behind the newest frame
        p.frames(1);
      }
      expect(fieldBoss.getState().hp).toEqual({ hp: 650, maxHp: 1000 });
      make.mockRestore();
    });

    it('show an HP bar over a mob’s head while it’s locked or for 5 s after it last lost HP, an elite’s in gold, none over a boss or a killed one', async () => {
      const p = await playing(FLAT, 4, 20);
      const bar = (id: number) => p.parent.querySelector<HTMLElement>(`[data-bar="${id}"]`);
      const all = [mob(1, 'green_slime', 6), mob(2, 'sun_slime', 8), mob(3, 'king_slime', 10)];
      frames(p, 2, 8, all);
      expect(target.getState().target?.ref).toBe('m:1'); // the nearest, locked as it's seen
      expect([bar(1) !== null, bar(2), bar(3)]).toEqual([true, null, null]); // the lock's alone: none hurt yet, however far down
      const hurt = all.map((m) => ({ ...m, hp: 28 }));
      frames(p, 9, 12, hurt);
      expect(bar(1)?.style.display).toBe('');
      expect((bar(1)?.firstElementChild as HTMLElement).style.width).toBe(`${(28 / 34) * 100}%`);
      expect([bar(1)?.style.outline, bar(2)?.style.outline]).toEqual(['', '2px solid #f2c14e']);
      expect(bar(3)).toBeNull(); // the boss bar at the top shows its HP
      frames(p, 13, 13 + 5000 / FRAME_MS, hurt); // 5 s on, none hurt again
      expect([bar(1) !== null, bar(2)]).toEqual([true, null]); // the lock's kept
      const healed = { ...hurt[1]!, hp: 30 };
      const nearer = mob(4, 'green_slime', 5);
      const king = hurt[2]!;
      frames(p, 330, 333, [
        { ...mob(1, 'green_slime', 6, MobPose.DOWN), hp: 0 },
        nearer,
        healed,
        king,
      ]);
      expect(target.getState().target?.ref).toBe('m:4'); // the lock moves on
      expect([bar(1), bar(2)]).toEqual([null, null]); // killed, and a heal's no hurt
      p.push('delta', { removedMobs: [2] }, { tick: 334 });
      p.frames(4);
      frames(p, 335, 338, [nearer, { ...healed, hp: 20 }, king]);
      expect(bar(2)).toBeNull(); // back in sight, seen anew
      changeMap(p, { mapId: FLAT, x: 4, y: 0, z: 20, yaw: 0 }); // as a rejoin
      p.push(
        'snapshot',
        { players: [standing(4, 20)], mobs: [nearer, { ...healed, hp: 10 }] },
        { tick: 339 },
      );
      await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
      frames(p, 340, 343, [nearer, { ...healed, hp: 10 }]);
      frames(p, 344, 347, [
        { ...nearer, hp: 25 },
        { ...healed, hp: 10 },
      ]);
      expect([bar(4) !== null, bar(2)]).toEqual([true, null]); // one hurt on the new map, and one seen there anew
    });

    it('play a boss’s attack as the drawn frames reach its windup, laying its area then', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'king_slime', 6)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob_Crown.glb')).toBeDefined(), WAIT);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      const telegraphs = () => meshesOn(p, 'patch').filter((m) => m.visibility > 0).length;
      frames(p, 11, 11, [mob(1, 'king_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob_Crown.glb'), 'CharacterArmature|Bite_Front')).toBe(
        0,
      ); // drawn 100 ms behind, it hasn't begun
      expect(telegraphs()).toBe(0);
      frames(p, 12, 16, [mob(1, 'king_slime', 6)]);
      expect(telegraphs()).toBe(2); // its landing's area, and what has filled of it
      expect(weight(p.root('quaternius/GreenBlob_Crown.glb'), 'CharacterArmature|Bite_Front')).toBe(
        0,
      ); // its bite waits to strike on the landing
      frames(p, 17, 26, [mob(1, 'king_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob_Crown.glb'), 'CharacterArmature|Bite_Front')).toBe(
        1,
      );
      frames(p, 27, 40, [mob(1, 'king_slime', 6)]);
      expect(meshesOn(p, 'patch')).toHaveLength(0);
    });

    it('lay no area for a normal mob’s attack, read from its body: its bite still strikes as the drawn frames reach it', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'green_slime', 6)]);
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      const telegraphs = () => meshesOn(p, 'patch').filter((m) => m.visibility > 0).length;
      frames(p, 11, 11, [mob(1, 'green_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(0); // drawn 100 ms behind, it hasn't begun
      expect(telegraphs()).toBe(0);
      frames(p, 12, 16, [mob(1, 'green_slime', 6)]);
      expect(telegraphs()).toBe(0); // blocking.md section 2: only a boss marks the ground
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(0); // its bite waits to strike on the landing
      frames(p, 17, 26, [mob(1, 'green_slime', 6)]);
      expect(weight(p.root('quaternius/GreenBlob.glb'), 'CharacterArmature|Bite_Front')).toBe(1);
      frames(p, 27, 40, [mob(1, 'green_slime', 6)]);
      expect(meshesOn(p, 'patch')).toHaveLength(0); // none was made
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
      frames(p, 2, 10, [mob(1, 'king_slime', 6)]);
      const LEAD = 3; // ticks the player's inputs run ahead of the frames they hear
      const first = p.ws
        .frames()
        .filter((f) => f.t === 'input')
        .at(-1)!.seq; // runs at tick 11 + LEAD
      const hear = (tick: number) => {
        const ackSeq = first + tick - 11 - LEAD; // the input that ran at tick
        p.push(
          'delta',
          { mobs: [mob(1, 'king_slime', 6)], players: [standing(4, 20)], ackSeq },
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
      const patches = () => meshesOn(p, 'patch').filter((m) => m.visibility > 0);
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
      frames(p, 2, 10, [mob(1, 'king_slime', 6)]);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 15, [mob(1, 'king_slime', 6)]);
      expect(meshesOn(p, 'patch')).toHaveLength(2);
      frames(p, 16, 20, [mob(1, 'king_slime', 6, MobPose.FLINCH)]);
      expect(meshesOn(p, 'patch')).toHaveLength(0);
    });
  });

  describe('hits', () => {
    it('spark on the mob struck and pop a number over it: a crit gold and bigger, a miss grey', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      const sparks = () => nodesOn(p, 'spark');
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
      const [spark] = nodesOn(p, 'spark');
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
      const [spark] = nodesOn(p, 'spark');
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

    it('show BLOCK over the player for a blow their guard stopped, with a clang on the guard’s ring toward the mob', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push('damage', { source: 'm:1', target: `p:${ME}`, block: true }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([['miss', 'BLOCK']]);
      const [spark] = nodesOn(p, 'spark');
      const ring = new Vector3(4 + GUARD_M, movement.height / 2, 20); // the guard's ring from the player's middle toward the boar, east
      expect(Vector3.Distance(spark!.position, ring)).toBeCloseTo(0.1, 3); // brought out toward the camera by its 0.1 m
    });

    it('show a hit past the player’s shield as what came off HP, then what the shield took', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push('damage', { source: 'm:1', target: `p:${ME}`, amount: 9, shielded: 4 }, { tick: 11 });
      p.frames(1);
      expect(numbers(p)).toEqual([
        ['taken', '-5'],
        ['shield', '-4'],
      ]);
    });

    it('show a damage-over-time tick past the player’s shield as its HP part in its status’s colour, then the shield’s', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'wild_boar', 6)]);
      p.push(
        'damage',
        { source: 'm:1', target: `p:${ME}`, amount: 5, statusId: 'poison', shielded: 3 },
        { tick: 11 },
      );
      p.frames(1);
      expect(numbers(p)).toEqual([
        ['dot', '-2'],
        ['shield', '-3'],
      ]);
      const hp = p.parent.querySelector<HTMLElement>('[data-number="dot"]')!;
      expect(hp.style.color).toBe(rgb(statusColor('poison')));
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

  it('readies a fight’s shaders as the map starts, out of sight: every look played off the map and gone again, and the outline’s before any lock', async () => {
    const p = await playing(FLAT, 4, 20);
    const offMap = () => p.scene()!.meshes.filter((m) => m.isEnabled() && !onMap(m)).length;
    expect(offMap()).toBeGreaterThan(0);
    for (let i = 0; i < 200 && offMap() > 0; i++) {
      p.live(1);
    }
    expect([
      offMap(),
      builtShaders(p.scene()!.getEngine()).some((k) => k.startsWith('glowMapGeneration')),
    ]).toEqual([0, true]);
  });

  /** A pouch of the player's at (x, 0, 20) holding one of each template. */
  const pouch = (id: number, kind: PouchKind, x: number, ...templates: string[]) =>
    create(PouchViewSchema, {
      id,
      kind,
      x,
      z: 20,
      items: templates.map((template, i) => ({ item: i + 1, template, quantity: 1 })),
      goneInMs: 60000n,
    });

  it('draws the player’s pouches where they fell, a kill’s as the sack and a box as the chest, gold for a 4★ item, and takes each away once gone', async () => {
    const p = await playing(FLAT, 4, 20);
    pouches
      .getState()
      .applyDelta(
        [
          pouch(3, PouchKind.POUCH, 10, 'slime_gel'),
          pouch(4, PouchKind.BOX, 14, 'slime_gel'),
          pouch(5, PouchKind.BOX, 18, 'slime_gel', 'gold_ring'),
        ],
        [],
      );
    const drawn = (id: number) => {
      const root = p.scene()!.transformNodes.find((n) => n.name === `pouch ${id}`);
      return (
        root && {
          at: root.position.asArray(),
          meshes: root.getChildMeshes(false).map((m) => m.name),
        }
      );
    };
    await vi.waitFor(() => expect(drawn(5)?.meshes.length).toBeGreaterThan(0), WAIT);
    expect(drawn(3)?.at).toEqual([10, 0, 20]);
    expect(drawn(3)?.meshes).toContain('pouch 3 sack');
    expect(drawn(4)?.meshes).toContain('pouch 4 chest_lid');
    expect(drawn(5)?.meshes).toContain('pouch 5 chest_gold_lid');
    pouches.getState().applyDelta([], [3, 5]);
    expect([drawn(3), drawn(4)?.at, drawn(5)]).toEqual([undefined, [14, 0, 20], undefined]);
  });

  it('prompts the loot key, as bound, over the nearest pouch in reach of the player’s middle while the loot window is shut', async () => {
    const p = await playing(FLAT, 4, 20);
    pouches.getState().applyDelta([pouch(3, PouchKind.POUCH, 10, 'slime_gel')], []);
    p.frames(1);
    expect([pouches.getState().inReach, p.label('loot-prompt')]).toEqual([[], '']);
    pouches.getState().applyDelta([pouch(4, PouchKind.POUCH, 5.5, 'slime_gel')], []); // 1.5 m away, its middle 0.875 m up: 1.74 m in all
    p.frames(1);
    expect([pouches.getState().inReach, p.label('loot-prompt')]).toEqual([[4], 'T Loot']);
    settings.getState().setKeys({ loot: 'KeyG' });
    p.frames(1);
    expect(p.label('loot-prompt')).toBe('G Loot');
    ui.getState().openWindow('loot');
    p.frames(1);
    expect(p.label('loot-prompt')).toBe('');
  });

  it('the loot key opens the window on the pouches in reach, pressed again takes all they hold in one TakeLoot, and walking out of reach closes it', async () => {
    const p = await playing(FLAT, 4, 20);
    const press = () => {
      key('KeyT', true);
      key('KeyT', false);
    };
    press();
    expect(ui.getState().windows).toEqual([]); // nothing in reach
    pouches
      .getState()
      .applyDelta(
        [
          pouch(4, PouchKind.POUCH, 5, 'slime_gel', 'red_potion'),
          pouch(5, PouchKind.BOX, 3, 'gold_ring'),
          pouch(6, PouchKind.POUCH, 30, 'slime_gel'),
        ],
        [],
      );
    p.frames(1);
    press();
    expect([ui.getState().windows, p.sent('takeLoot')]).toEqual([['loot'], []]);
    press();
    expect(p.sent('takeLoot')).toMatchObject([
      {
        picks: [
          { pouch: 4, item: 1 },
          { pouch: 4, item: 2 },
          { pouch: 5, item: 1 },
        ],
      },
    ]);
    press(); // each pick in flight already: nothing asked twice
    expect(p.sent('takeLoot')).toHaveLength(1);
    pouches.getState().applyDelta([], [4, 5]);
    p.frames(1);
    expect(ui.getState().windows).toEqual([]);
  });

  it('plays on the keys the player bound, read at each press', async () => {
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
    settings.getState().setKeys({ skill1: 'KeyB', attack: 'KeyQ' });
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('attack')).toHaveLength(1);
    p.frames(40);
    key('KeyX', true); // X attacks no more
    key('KeyX', false);
    key('KeyB', true);
    key('KeyB', false);
    for (let waited = 0; p.sent('useSkill').length === 0 && waited < 20; waited++) {
      p.frames(1);
    }
    expect([p.sent('attack'), p.sent('useSkill')]).toEqual([
      [expect.anything()],
      [expect.objectContaining({ skill: 'strike' })],
    ]);
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
    key('KeyQ', true);
    key('KeyQ', false);
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
      meshesOn(p, 'ring').filter((m) => m.isEnabled() && m.visibility > 0).length;
    const channel = () => {
      teleport.getState().start({ map: 'cave' });
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
    expect([teleport.getState().to, glowing()]).toEqual([{ map: 'cave' }, 2]);
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
    key('KeyQ', true);
    key('KeyQ', false);
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
    for (const code of ['KeyX', 'KeyQ']) {
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

  it('breaks free from a stun on the escape key: it sends Unyielding from Roll Back’s key, granted with no points, and walks the player from the next input before any frame ends the stun', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 10,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { roll_back: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['roll_back']);
    const stunned = {
      ...standing(4, 20),
      held: 60,
      statuses: [create(StatusViewSchema, { id: 'stun', ends: 30 })],
    };
    p.frames(2);
    p.push('delta', { players: [stunned], ackSeq: p.inputs() - 1 }, { tick: 3 });
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('unyielding');
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.chainRoot])).toEqual([
      ['unyielding', 'roll_back'],
    ]);
    key('ArrowRight', true);
    p.frames(3);
    key('ArrowRight', false);
    expect(p.root(classLook('warrior').file)!.position.x).toBeGreaterThan(4.2);
  });

  it('stunned with Unyielding cooling, Roll Back’s key still offers it, showing its cooldown, and a press sends nothing; pinned, the key is Roll Back again, which still goes', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 10,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { roll_back: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['roll_back']);
    skillBar.getState().cool('unyielding', { from: 0, until: Number.MAX_SAFE_INTEGER });
    const heldBy = (id: string, tick: number) => {
      const me = {
        ...standing(4, 20),
        held: 60,
        statuses: [create(StatusViewSchema, { id, ends: 30 })],
      };
      p.push('delta', { players: [me], ackSeq: p.inputs() - 1 }, { tick });
      p.frames(1);
    };
    p.frames(2);
    heldBy('stun', 3);
    expect(skillBar.getState().live[0]).toBe('unyielding');
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill')).toHaveLength(0);
    heldBy('pin', 4);
    expect(skillBar.getState().live[0]).toBe('roll_back');
  });

  it('a stunned Magician’s Arcane Release, on Blink’s key, blinks 4 m the way they walk from the next input, though the frames still hold them', async () => {
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
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { blink: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['blink']);
    const stunned = {
      ...standing(4, 20, ME, 'magician'),
      held: 60,
      statuses: [create(StatusViewSchema, { id: 'stun', ends: 30 })],
    };
    p.frames(2);
    p.push('delta', { players: [stunned], ackSeq: p.inputs() - 1 }, { tick: 3 });
    key('ArrowRight', true);
    p.frames(1);
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.chainRoot])).toEqual([
      ['arcane_release', 'blink'],
    ]);
    p.frames(2);
    key('ArrowRight', false);
    expect(p.root(classLook('magician').file)!.position.x).toBeGreaterThan(8);
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
    key('KeyQ', true);
    key('KeyQ', false);
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
    teleport.getState().start({ map: 'forest' });
    key('KeyX', true);
    key('KeyX', false);
    p.frames(2);
    expect([p.sent('attack'), teleport.getState().to]).toEqual([[], { map: 'forest' }]);
  });

  it('in a match’s prep the wall across the ring stands, drawn and walked against, and attacks and skills wait; the round takes it down', async () => {
    const holds = vi.spyOn(Predictor.prototype, 'holdFor');
    const p = await playing('ring', 22, 18);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: 'ring',
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 30,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { strike: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    const wallUp = () => meshesOn(p, 'prep wall').some((m) => m.isEnabled());
    const phase = (phase: MatchPhase) =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'duel',
          phase,
          round: 1,
          endsInMs: 10_000,
        }),
        0,
      );
    p.live(1);
    expect(wallUp()).toBe(false); // no match yet
    phase(MatchPhase.PREP);
    key('ArrowUp', true);
    p.live(60);
    const me = p.root(classLook('warrior').file)!;
    expect(wallUp()).toBe(true);
    expect(holds).toHaveBeenLastCalledWith({ from: 200, until: 260 }); // the count after prep's 10 s, held from its first tick
    expect(me.position.z).toBeCloseTo(22 - 0.3 - movement.radius, 3); // the wall's south face
    key('KeyX', true);
    key('KeyX', false);
    key('KeyQ', true);
    key('KeyQ', false);
    expect([p.sent('attack'), p.sent('useSkill')]).toEqual([[], []]);
    expect(toasts.getState().toasts.map((t) => t.text)).toContain('Wait for the round to start.');
    phase(MatchPhase.FIGHT);
    p.live(40);
    key('ArrowUp', false);
    expect(wallUp()).toBe(false);
    expect(holds).toHaveBeenLastCalledWith(null);
    holds.mockRestore();
    expect(me.position.z).toBeGreaterThan(23); // through where it stood
    key('KeyX', true);
    key('KeyX', false);
    expect(p.sent('attack')).toHaveLength(1); // a match's map fights as a hunting map does
  });

  it('takes a solid at the camera’s end of its line to the player from the view, 8 m from the player, and gives it back as the player walks clear', async () => {
    expect(TOWERED).toBeDefined();
    const p = await playing(TOWERED, 60, 40);
    p.live(2);
    const tower = () => meshesOn(p, 'tower')[0]!.material!.name;
    expect(tower()).toMatch(/ cut 0$/); // the camera 10.3 m south and 12.3 m up, 1.2 m from its face
    p.push('snapshot', { players: [standing(20, 40)] }, { tick: 100 });
    p.live(4);
    expect(tower()).not.toMatch(/ 0(\.\d+)?$/);
  });

  it('in a battle’s fight the player walks their side’s ground: the other side’s base stands, their own doesn’t, and again once the map is entered afresh', async () => {
    const p = await playing('battlefield', 112, 45);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: 'battlefield',
        character: { id: ME, name: 'Ayla', class: 'warrior', level: 30, hp: 50, maxHp: 50 },
      }),
    );
    const onSide = (side: number) =>
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'battle',
          phase: MatchPhase.FIGHT,
          endsInMs: 600_000,
          sides:
            side === 1
              ? [{ fighters: [{ id: ME }] }, { fighters: [{ id: 'c-robin' }] }]
              : [{ fighters: [{ id: 'c-robin' }] }, { fighters: [{ id: ME }] }],
        }),
        0,
      );
    onSide(1);
    key('ArrowRight', true);
    p.live(60);
    const me = p.root(classLook('warrior').file)!;
    expect(me.position.x).toBeCloseTo(127 - 9 - movement.radius, 3); // side 2's base, at (127, 45), 9 m round
    onSide(2);
    p.live(20);
    key('ArrowRight', false);
    expect(me.position.x).toBeGreaterThan(120); // on into its own
    onSide(1);
    p.live(1); // tick 82
    changeMap(p, { mapId: 'battlefield', x: 112, y: 0, z: 45, yaw: 0 }); // as a return to the same battle
    p.push('snapshot', { players: [standing(112, 45)] }, { tick: 82 });
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
    key('ArrowRight', true);
    p.live(60);
    key('ArrowRight', false);
    expect(p.root(classLook('warrior').file)!.position.x).toBeCloseTo(127 - 9 - movement.radius, 3);
  });

  it('on a battle’s map rings each point on the ground in its holder’s colour from the player’s side, the name tags’ pair, grey with no match; the other side’s base is walled in red through the fight', async () => {
    const p = await playing('battlefield', 20, 45);
    const one = (name: string) => {
      const [m] = meshesOn(p, name);
      return m!;
    };
    const decal = (hex: string, alpha: number) => `decal ${hex} ${alpha}`;
    const glow = (hex: string) => decal(Color3.FromHexString(hex).scale(0.45).toHexString(), 0.2);
    const looks = () =>
      ['west', 'middle', 'east'].map((id) => [
        one(`point ring ${id}`).material!.name,
        one(`point glow ${id}`).material!.name,
      ]);
    const walls = () => [1, 2].map((i) => one(`base wall ${i}`).isEnabled());
    p.frames(1);
    const grey = [decal('#D8D8D8', 0.9), glow('#d8d8d8')];
    expect(looks()).toEqual([grey, grey, grey]);
    expect(walls()).toEqual([false, false]);
    const ring = one('point ring east');
    ring.refreshBoundingInfo({});
    const box = ring.getBoundingInfo().boundingBox;
    expect([ring.position.x, ring.position.z, box.extendSize.x, box.extendSize.z]).toEqual([
      102, 45, 5, 5,
    ]); // round the east point, 5 m out
    expect(box.minimumWorld.y).toBeCloseTo(0.04, 3); // just over the flat ground
    const wall = one('base wall 2');
    wall.refreshBoundingInfo({});
    expect([wall.position.x, wall.getBoundingInfo().boundingBox.extendSize.x]).toEqual([127, 9]);
    pvp.getState().applyMatchState(
      create(MatchStateSchema, {
        match: 'mt-1',
        mode: 'battle',
        phase: MatchPhase.FIGHT,
        points: [
          { id: 'west', holder: 1 },
          { id: 'middle', holder: 2, contested: true },
        ],
        sides: [{ fighters: [{ id: ME }] }, { fighters: [{ id: 'c-robin' }] }],
      }),
      0,
    );
    p.frames(1);
    expect(looks()).toEqual([
      [decal('#7FE08A', 0.9), glow('#7fe08a')],
      [decal('#FF6B62', 0.9), glow('#ff6b62')],
      grey,
    ]);
    expect(walls()).toEqual([false, true]); // side 1 kept from base 2
    expect(one('base wall 2').material!.name).toBe('base wall');
  });

  it.each([
    { code: 'KeyM', window: 'map' },
    { code: 'KeyV', window: 'arena' },
  ])('$code opens the $window window and closes it again', async ({ code, window }) => {
    await playing(FLAT, 10, 20);
    key(code, true);
    key(code, false);
    expect(ui.getState().windows).toEqual([window]);
    key(code, true);
    key(code, false);
    expect(ui.getState().windows).toEqual([]);
  });

  it('tells the map views the map, and about ten times a second the player, the party members here, the lock and the field boss while it’s up', async () => {
    const p = await playing(FLAT, 10, 20);
    expect([mapView.getState().mapId, mapView.getState().size]).toEqual([FLAT, [72, 40]]);
    expect(mapView.getState().ground.kind).toBe('image'); // a map's own
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
    fieldBoss.getState().applyFieldBoss(create(FieldBossSchema, { mobId: 1, type: 'green_slime' }));
    frames(p, 15, 16, pack);
    expect(marks().boss).toEqual({ x: 13, z: 20 });
    fieldBoss
      .getState()
      .applyGone(create(FieldBossGoneSchema, { mobId: 1, type: 'green_slime', backInMs: 60000n }));
    frames(p, 17, 18, pack);
    expect(marks().boss).toBeNull();
  });

  describe('a party member out of sight', () => {
    const party3 = (robin: { mapId: string; online: boolean }) =>
      create(PartyStateSchema, {
        party: create(PartyViewSchema, {
          leader: ME,
          members: [
            { characterId: ME, name: 'Ayla', mapId: REGIONED, online: true },
            { characterId: 'c-robin', name: 'Robin', ...robin },
            { characterId: 'c-jade', name: 'Jade', mapId: 'cave', online: true },
          ],
        }),
      });
    const spots = (...list: [number, number][]) =>
      party.getState().applyPartySpots(
        create(PartySpotsSchema, {
          spots: list.map(([x, z]) => ({ characterId: 'c-robin', x, z })),
        }),
      );
    const seen = () => ({
      marked: mapView.getState().party.map((q) => [q.id, q.x, q.z]),
      statuses: statuses.getState().party,
    });
    const on = async (mapId: string) => {
      const p = await playing(mapId, 10, 20);
      party.getState().applyPartyState(party3({ mapId, online: true }));
      let tick = 1;
      const frame = (d: ServerInit<'delta'> = {}) => {
        p.push('delta', d, { tick: ++tick });
        p.frames(1);
      };
      const frames = (n: number, d?: ServerInit<'delta'>) => {
        frame(d);
        for (let i = 1; i < n; i++) {
          frame();
        }
      };
      return frames;
    };

    it('stays on the map views and says so on a map cut by cells: where last drawn till the server speaks, then as long as it lists them, online and on the map', async () => {
      const frames = await on(REGIONED);
      frames(7, { players: [standing(16, 22, 'c-robin', 'thief')] });
      expect(seen()).toEqual({ marked: [['c-robin', 16, 22]], statuses: { 'c-robin': [] } });
      frames(5, { unseen: { players: ['c-robin'] } });
      expect(seen()).toEqual({ marked: [['c-robin', 16, 22]], statuses: { 'c-robin': null } }); // where last drawn
      spots([50, 30]);
      frames(2);
      expect(seen()).toEqual({ marked: [['c-robin', 50, 30]], statuses: { 'c-robin': null } });
      spots(); // to another channel of the map: not here, though on it
      frames(2);
      expect(seen()).toEqual({ marked: [], statuses: {} });
      spots([60, 30]);
      frames(2);
      expect(seen()).toEqual({ marked: [['c-robin', 60, 30]], statuses: { 'c-robin': null } });
      party.getState().applyPartyState(party3({ mapId: REGIONED, online: false }));
      frames(2);
      expect(seen()).toEqual({ marked: [], statuses: {} });
      party.getState().applyPartyState(party3({ mapId: 'cave', online: true }));
      frames(2);
      expect(seen()).toEqual({ marked: [], statuses: {} });
      party.getState().applyPartyState(party3({ mapId: REGIONED, online: true }));
      frames(6, { players: [standing(20, 24, 'c-robin', 'thief')] });
      expect(seen()).toEqual({ marked: [['c-robin', 20, 24]], statuses: { 'c-robin': [] } });
    });

    it('is marked only once the server lists them, if they went out of view before joining the party', async () => {
      const frames = await on(REGIONED);
      party.getState().applyPartyState(create(PartyStateSchema, {}));
      frames(7, { players: [standing(16, 22, 'c-robin', 'thief')] });
      frames(5, { unseen: { players: ['c-robin'] } });
      party.getState().applyPartyState(party3({ mapId: REGIONED, online: true }));
      frames(2);
      expect(seen()).toEqual({ marked: [], statuses: {} });
      spots([50, 30]);
      frames(2);
      expect(seen()).toEqual({ marked: [['c-robin', 50, 30]], statuses: { 'c-robin': null } });
    });

    it('drops off the map views at once when the party says they’re in another of the region’s channels', async () => {
      const p = await playing(REGIONED, 10, 20);
      p.push('snapshot', { players: [standing(10, 20)], channel: 2 }, { tick: 2 });
      const robinIn = (channel: number) =>
        party.getState().applyPartyState(
          create(PartyStateSchema, {
            party: create(PartyViewSchema, {
              leader: ME,
              members: [
                { characterId: ME, name: 'Ayla', mapId: REGIONED, online: true, channel: 2 },
                { characterId: 'c-robin', name: 'Robin', mapId: REGIONED, online: true, channel },
              ],
            }),
          }),
        );
      robinIn(2);
      let tick = 2;
      const frames = (n: number, d: ServerInit<'delta'> = {}) => {
        for (let i = 0; i < n; i++) {
          p.push('delta', i === 0 ? d : {}, { tick: ++tick });
          p.frames(1);
        }
      };
      frames(7, { players: [standing(16, 22, 'c-robin', 'thief')] });
      frames(5, { unseen: { players: ['c-robin'] } });
      expect(seen().marked).toEqual([['c-robin', 16, 22]]); // where last drawn
      robinIn(3); // switched, before the server's spots say anything
      frames(2);
      expect(seen()).toEqual({ marked: [], statuses: {} });
    });

    it('drops off a map sent whole, where everyone on it is drawn', async () => {
      const frames = await on(FLAT);
      frames(7, { players: [standing(16, 22, 'c-robin', 'thief')] });
      expect(seen()).toEqual({ marked: [['c-robin', 16, 22]], statuses: { 'c-robin': [] } });
      frames(5, { removedPlayers: ['c-robin'] }); // to another channel, or gone with the frame's news still to come
      expect(seen()).toEqual({ marked: [], statuses: {} });
    });
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

  it('draws in the outline’s pass only what could cover the locked mob: not a mob off its line', async () => {
    const p = await playing(FLAT, 10, 20);
    frames(p, 2, 10, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 18)]);
    await vi.waitFor(() => expect(p.root('quaternius/Blob_Dog.glb')).toBeDefined(), WAIT);
    frames(p, 11, 14, [mob(1, 'green_slime', 4), mob(2, 'wild_boar', 18)]);
    expect(outlined(p, 'quaternius/GreenBlob.glb')).toBe(true);
    const layer = p.scene()!.effectLayers[0] as HighlightLayer;
    const drawn = new Set<AbstractMesh>();
    layer.onBeforeRenderMeshToEffect.add((m) => drawn.add(m));
    p.frames(2);
    const of = (file: string) =>
      p
        .root(file)!
        .getChildMeshes()
        .filter((m) => drawn.has(m)).length;
    expect([of('quaternius/GreenBlob.glb') > 0, of('quaternius/Blob_Dog.glb')]).toEqual([true, 0]);
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
    expect(target.getState().target).toEqual({
      kind: 'mob',
      ref: 'm:1',
      type: 'green_slime',
      hp: 30,
    });
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
    expect(target.getState().target?.ref).toBe('m:2'); // the window with it
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
      [expect.closeTo(-Math.PI / 2, 5), 'm:2'],
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
    const locked = () => target.getState().target?.ref ?? null;
    frames(p, 5, 6, [slime((edge + off) / 2)]);
    await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
    frames(p, 7, 8, [slime((edge + off) / 2)]);
    expect(locked()).toBeNull(); // the nearest mob, but at the edge
    frames(p, 9, 10, [slime((edge + off) / 2), boar]);
    expect(locked()).toBe('m:2'); // the farther one in view
    frames(p, 11, 12, [slime(18), boar]);
    key('Tab', true);
    key('Tab', false);
    frames(p, 13, 14, [slime(18), boar]);
    expect(locked()).toBe('m:1');
    frames(p, 15, 16, [slime((edge + off) / 2), boar]);
    expect(locked()).toBe('m:1'); // held at the edge
    frames(p, 17, 18, [slime(off - 0.5), boar]);
    expect(locked()).toBe('m:2'); // off the canvas: dropped, and the one in view locked
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
    expect(p.sent('attack').map((d) => [d.aim, d.target])).toEqual([[expect.closeTo(0, 5), 'm:1']]);
    p.frames(Math.ceil(attack.strikeEnd / FRAME_MS) - 1);
    expect(off(me.rotation.y, 0)).toBeLessThan(0.2); // still striking: toward the lock
    p.frames(4);
    expect(off(me.rotation.y, Math.PI / 2)).toBeLessThan(0.05); // struck: back along the heading
    const slash = nodesOn(p, 'band')[0];
    expect(slash && off(slash.rotation.y, 0)).toBeLessThan(1e-6); // the slash still where the hit went
    p.frames(Math.ceil(attack.end / FRAME_MS));
    const skill = beatsOf(skillTiming(skillById('strike')!.stages[0]!, FRAME_MS), 0, FRAME_MS);
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.aim, d.target])).toEqual([
      ['strike', expect.closeTo(0, 5), 'm:1'],
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
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.chainRoot, d.target])).toEqual([
      ['charge', 'strike', 'm:1'],
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

  it('a block’s press raises the guard on the player’s body, not the class’s skill, and its half ring', async () => {
    const p = await playing(FLAT, 4, 20);
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'warrior',
          level: 10,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: {} } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['bastion']);
    const me = p.root(classLook('warrior').file);
    key('KeyQ', true);
    key('KeyQ', false);
    p.frames(2);
    expect(p.sent('useSkill').map((d) => d.skill)).toEqual(['bastion']);
    expect([weight(me, 'Block'), weight(me, '1H_Melee_Attack_Chop')]).toEqual([1, 0]);
    expect(p.scene()!.meshes.some((m) => m.name === 'band' && m.isEnabled())).toBe(true);
  });

  it('a block’s key offers its counter from the frame that carried the player’s own BLOCK, to the window’s last input', async () => {
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
          sheet: { skills: { unspent: 0, invested: { ground_slam: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['bastion']);
    const boar = mob(1, 'wild_boar', 12);
    frames(p, 2, 12, [boar]);
    expect(skillBar.getState().live[0]).toBe('bastion');
    const acked = p.inputs() - 1;
    p.push('delta', { ackSeq: acked, mobs: [boar] }, { tick: 13 });
    p.push('damage', { source: 'm:1', target: 'p:c-robin', block: true }, { tick: 13 }); // another's
    p.push('damage', { source: 'm:1', target: `p:${ME}`, dodge: true }, { tick: 13 }); // a dodge isn't a block
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('bastion');
    p.push('damage', { source: 'm:1', target: `p:${ME}`, block: true }, { tick: 13 });
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('ground_slam');
    while (p.inputs() < acked + 30) {
      p.frames(1); // to the window's last input, 1.5 s on from the acked one
    }
    expect(skillBar.getState().live[0]).toBe('ground_slam');
    p.frames(1);
    expect(skillBar.getState().live[0]).toBe('bastion');
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
    key('KeyQ', true);
    key('KeyQ', false);
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

  it('draws a bubble round the player while the newest frame has shield left on them, and round another while their drawn frame does', async () => {
    const p = await playing(FLAT, 10, 20);
    const bubbles = () =>
      p
        .scene()!
        .meshes.filter((m) => m.name === 'shield bubble' && m.isEnabled())
        .map((m) => Math.round(m.position.x));
    let tick = 1;
    const frame = (mine: number, theirs: number) => {
      p.push(
        'delta',
        {
          players: [
            { ...standing(10, 20), shield: mine },
            { ...standing(16, 20, 'c-robin', 'thief'), shield: theirs },
          ],
        },
        { tick: ++tick },
      );
      p.frames(1);
    };
    frame(0, 0);
    await vi.waitFor(() => expect(p.root(classLook('thief').file)).toBeDefined(), WAIT);
    frame(0, 0);
    expect(bubbles()).toEqual([]);
    frame(40, 0);
    expect([bubbles(), character.getState().shield]).toEqual([[10], 40]); // the HP bar's pale part too
    frame(12, 30);
    expect(bubbles()).toEqual([10]); // theirs drawn behind the newest
    frame(12, 30);
    expect(bubbles().toSorted()).toEqual([10, 16]);
    frame(0, 0);
    frame(0, 0);
    expect([bubbles(), character.getState().shield]).toEqual([[], 0]);
  });

  it('centres an area at the lock on the mob it was aimed at: the player’s own press’s lock, and the one another’s SkillUsed names, by its trait too', async () => {
    const p = await playing(FLAT, 10, 20, 'bowman');
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'bowman',
          level: 20,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { power_shot: 1, arrow_rain: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['power_shot']);
    const slime = {
      ...mob(1, 'green_slime', 10),
      z: 26,
      statuses: [create(StatusViewSchema, { id: 'stun', ends: 10_000 })],
    }; // 6 m north, within Arrow Rain's 14, stunned: Power Shot's key offers it
    let tick = 1;
    const frame = () => {
      p.push(
        'delta',
        { mobs: [slime], players: [standing(14, 20, 'c-robin', 'magician')] },
        { tick: ++tick },
      );
      p.frames(1);
    };
    frame();
    await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
    await vi.waitFor(() => expect(p.root(classLook('magician').file)).toBeDefined(), WAIT);
    frame();
    frame();
    const centres = () =>
      meshesOn(p, 'patch')
        .filter((m) => m.isEnabled())
        .map((m) => {
          const v = m.getVerticesData(VertexBuffer.PositionKind)!;
          const xs = v.filter((_, i) => i % 3 === 0);
          const zs = v.filter((_, i) => i % 3 === 2);
          return [
            Math.round(Math.min(...xs) + Math.max(...xs)) / 2,
            Math.round(Math.min(...zs) + Math.max(...zs)) / 2,
          ];
        });
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.target])).toEqual([['arrow_rain', 'm:1']]);
    frame();
    expect(centres()).toEqual([
      [10, 26],
      [10, 26],
    ]); // its rim and fill
    p.push(
      'skillUsed',
      { caster: 'c-robin', skill: 'meteor', stage: 0, aim: 0, target: 'm:1' },
      { tick },
    );
    p.push(
      'skillUsed',
      {
        caster: 'c-robin',
        skill: 'frost_nova',
        stage: 1,
        aim: 0,
        target: 'm:1',
        trait: 'ice_prison',
      },
      { tick },
    );
    frame();
    expect(centres()).toEqual(Array(4).fill([10, 26])); // the player's, and Ice Prison's in place of Meteor's, which its caster's next skill ends; Frost Nova's own is an aura round the caster
  });

  it('in a match centres an area at the lock on the enemy it was aimed at: the player’s own press’s, and another’s SkillUsed at the player', async () => {
    const p = await playing(FLAT, 10, 20, 'bowman');
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'bowman',
          level: 30,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: { power_shot: 1, arrow_rain: 1 } } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['power_shot']);
    const robin = {
      ...standing(10, 26, 'c-robin', 'magician'),
      team: 2,
      statuses: [create(StatusViewSchema, { id: 'stun', ends: 10_000 })],
    }; // 6 m north, stunned: Power Shot's key offers Arrow Rain
    let tick = 1;
    const frame = () => {
      p.push(
        'delta',
        { players: [{ ...standing(10, 20, ME, 'bowman'), team: 1 }, robin] },
        { tick: ++tick },
      );
      p.frames(1);
    };
    frame();
    await vi.waitFor(() => expect(p.root(classLook('magician').file)).toBeDefined(), WAIT);
    frame();
    frame();
    const centres = () =>
      meshesOn(p, 'patch')
        .filter((m) => m.isEnabled())
        .map((m) => {
          const v = m.getVerticesData(VertexBuffer.PositionKind)!;
          const xs = v.filter((_, i) => i % 3 === 0);
          const zs = v.filter((_, i) => i % 3 === 2);
          return [
            Math.round(Math.min(...xs) + Math.max(...xs)) / 2,
            Math.round(Math.min(...zs) + Math.max(...zs)) / 2,
          ];
        });
    key('KeyQ', true);
    key('KeyQ', false);
    expect(p.sent('useSkill').map((d) => [d.skill, d.target])).toEqual([
      ['arrow_rain', 'p:c-robin'],
    ]);
    frame();
    expect(centres()).toEqual([
      [10, 26],
      [10, 26],
    ]);
    p.push(
      'skillUsed',
      { caster: 'c-robin', skill: 'meteor', stage: 0, aim: Math.PI, target: `p:${ME}` },
      { tick },
    );
    frame();
    expect(centres()).toEqual([
      [10, 26],
      [10, 26],
      [10, 20],
      [10, 20],
    ]); // Robin's Meteor on the player it was aimed at
  });

  it('presses a skill with the trait the sheet holds, and times the player’s own buff with the trait its SkillUsed names', async () => {
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
            skills: {
              unspent: 0,
              invested: { strike: 25, war_cry: 10 },
              traits: { strike: 'concussion' },
            },
          },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['strike']);
    key('KeyQ', true);
    key('KeyQ', false);
    const { from, until } = skillBar.getState().cooldowns.strike!;
    expect(until - from).toBe(7000); // Concussion's
    p.push(
      'skillUsed',
      { caster: ME, skill: 'war_cry', stage: 1, aim: 0, trait: 'fury' },
      { tick: 3 },
    );
    expect(skillBar.getState().buffs).toEqual([
      { skill: 'war_cry', points: 10, trait: 'fury', until: from + 20_000 }, // Fury's 20 s, not Battle Roar's 40
    ]);
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
    key('KeyQ', true);
    key('KeyQ', false);
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

  it('draws another’s skill with its extra trait at its last evolution’s tier: Concussion cracks the ground as Crushing Blow does', async () => {
    const p = await playing(FLAT, 4, 20);
    for (let tick = 2; tick <= 8; tick++) {
      p.push('delta', tick === 2 ? { players: [standing(14, 20, 'c-robin', 'warrior')] } : {}, {
        tick,
      });
      p.frames(1);
    }
    await vi.waitFor(() => expect(p.root(classLook('warrior').file)).toBeDefined(), WAIT);
    // The ground's crack, at the caster's feet: the warm's lines are far off the map or at its corner.
    const cracks = (stage: number, trait: string, tick: number) => {
      p.push('skillUsed', { caster: 'c-robin', skill: 'strike', stage, aim: 0, trait }, { tick });
      p.frames(3);
      const n = meshesOn(p, 'line').filter((m) => {
        const c = m.getBoundingInfo().boundingBox.centerWorld;
        return Math.hypot(c.x - 14, c.z - 20) < 3;
      }).length;
      p.frames(80); // past its look
      return n;
    };
    expect([cracks(0, '', 30), cracks(2, '', 40), cracks(2, 'concussion', 50)]).toEqual([0, 1, 1]);
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

  it.each([
    ['the player’s', 2, 1],
    ['another', 3, 0],
  ])(
    'opens the heal key to a hurt party member not yet drawn in %s channel of the region, the world to decide, and no other',
    async (_, channel, sent) => {
      const p = await playing(REGIONED, 10, 20, 'magician');
      p.push('snapshot', { players: [standing(10, 20, ME, 'magician')], channel: 2 }, { tick: 2 });
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          token: 'tok-1',
          mapId: REGIONED,
          character: {
            id: ME,
            name: 'Ayla',
            class: 'magician',
            level: 10,
            hp: 50,
            maxHp: 50,
            sheet: { skills: { unspent: 0, invested: { heal: 1 } } },
          },
        }),
      );
      party.getState().applyPartyState(
        create(PartyStateSchema, {
          party: create(PartyViewSchema, {
            leader: ME,
            members: [
              {
                characterId: ME,
                name: 'Ayla',
                mapId: REGIONED,
                online: true,
                channel: 2,
                hp: 50,
                maxHp: 50,
              },
              {
                characterId: 'c-robin',
                name: 'Robin',
                mapId: REGIONED,
                online: true,
                channel,
                hp: 20,
                maxHp: 50,
              },
            ],
          }),
        }),
      );
      energy.getState().welcome(10);
      skillBar.getState().load(['heal']);
      frames(p, 3, 4, []);
      await Promise.allSettled(chunkWork.builds); // the ground holds the player till it comes
      await new Promise<void>((r) => setImmediate(r));
      frames(p, 5, 10, []);
      key('KeyQ', true);
      key('KeyQ', false);
      expect(p.sent('useSkill')).toHaveLength(sent);
    },
  );

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
    key('KeyQ', true);
    key('KeyQ', false);
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

    it('tells the HUD the player’s statuses, the lock’s and those of party members on this map, each timed from when it first shows, or restarts, to its end tick', async () => {
      const p = await playing(FLAT, 10, 20);
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
      const on = (id: string, ends: number) => [create(StatusViewSchema, { id, ends })];
      const robin = { ...standing(16, 22, 'c-robin'), statuses: on('slow', 50) };
      const send = (tick: number, me: PlayerWire, slime: MobWire) => {
        p.push('delta', { players: [me, robin], mobs: [slime] }, { tick });
        p.frames(1);
      };
      const stunned = { ...standing(10, 20), statuses: on('stun', 30) };
      const poisoned = { ...mob(1, 'green_slime', 13), statuses: on('poison', 40) }; // 3 m ahead: the lock
      send(2, stunned, poisoned);
      const first = statuses.getState().me;
      expect(first.map((s) => [s.id, s.until - s.from])).toEqual([['stun', 28 * 50]]); // 28 ticks of 50 ms
      for (let tick = 3; tick <= 10; tick++) {
        send(tick, stunned, poisoned);
      }
      await vi.waitFor(() => expect(p.root('quaternius/GreenBlob.glb')).toBeDefined(), WAIT);
      send(11, stunned, poisoned);
      send(12, stunned, poisoned);
      expect(target.getState().target?.ref).toBe('m:1');
      const now = statuses.getState();
      expect(now.me).toBe(first); // its timing kept, so its frame doesn't render again
      const stun = first[0]!.until;
      expect(now.target.map((s) => [s.id, s.until - stun])).toEqual([['poison', 10 * 50]]);
      expect(Object.keys(now.party)).toEqual(['c-robin']); // Jade is in the cave, beyond the frames
      expect(now.party['c-robin']!.map((s) => [s.id, s.until - stun])).toEqual([['slow', 20 * 50]]);
      send(13, { ...standing(10, 20), statuses: on('stun', 35) }, poisoned); // restarted
      const again = statuses.getState().me[0]!;
      expect([again.from > first[0]!.from, again.until - again.from]).toEqual([true, 22 * 50]);
      send(
        14,
        { ...standing(10, 20), statuses: on('stun', 35) },
        { ...mob(1, 'green_slime', 13), statuses: [] },
      );
      expect(statuses.getState().target).toEqual([]);
      p.push('delta', {}, { tick: 36 }); // past the stun's end, though no frame of the player says so
      p.frames(1);
      expect(statuses.getState().me).toEqual([]);
      expect(statuses.getState().party['c-robin']!.map((s) => s.id)).toEqual(['slow']);
    });

    it('tells a match’s frames each other fighter’s level, HP and statuses as of the newest frame, and who’s down; none outside a match', async () => {
      const p = await playing(FLAT, 10, 20);
      const on = (id: string, ends: number) => [create(StatusViewSchema, { id, ends })];
      const robin = {
        ...standing(14, 20, 'c-robin', 'magician'),
        team: 1,
        level: 28,
        hp: 30,
        statuses: on('slow', 50),
      };
      const ash = {
        ...standing(10, 30, 'c-ash', 'thief'),
        name: 'Ash',
        team: 2,
        level: 30,
        hp: 45,
      };
      const send = (tick: number, ...players: PlayerWire[]) => {
        p.push('delta', { players: [{ ...standing(10, 20), team: 1 }, ...players] }, { tick });
        p.frames(1);
      };
      send(2, robin, ash);
      expect(fighters.getState().live).toEqual({}); // no match
      pvp.getState().applyMatchState(
        create(MatchStateSchema, {
          match: 'mt-1',
          mode: 'team',
          phase: MatchPhase.FIGHT,
          round: 1,
          sides: [
            {
              fighters: [
                { id: ME, name: 'Ayla', class: 'warrior' },
                { id: 'c-robin', name: 'Robin', class: 'magician' },
              ],
            },
            {
              fighters: [
                { id: 'c-ash', name: 'Ash', class: 'thief' },
                { id: 'c-far', name: 'Far', class: 'bowman' },
              ],
            },
          ],
        }),
        0,
      );
      send(3, robin, ash);
      const live = fighters.getState().live;
      expect(Object.keys(live).sort()).toEqual(['c-ash', 'c-robin']); // not the player's own, nor one never drawn
      expect(live['c-robin']).toMatchObject({ level: 28, hp: 30, maxHp: 50, down: false });
      expect(live['c-robin']!.statuses.map((s) => [s.id, s.until - s.from])).toEqual([
        ['slow', 47 * 50],
      ]);
      expect(live['c-ash']).toMatchObject({ level: 30, hp: 45, down: false, statuses: [] });
      send(4, robin, { ...ash, dead: true });
      expect(fighters.getState().live['c-ash']).toMatchObject({ hp: 0, down: true });
      expect(fighters.getState().live['c-robin']).toBe(live['c-robin']); // unchanged, so its frame doesn't render again
    });

    it('pops a damage-over-time tick small in its status’s colour, with no spark or hold, leaving its inflicter’s windup to its own hit', async () => {
      const p = await playing(FLAT, 4, 20);
      frames(p, 2, 10, [mob(1, 'king_slime', 6)]);
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 5, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 16, [mob(1, 'king_slime', 6)]);
      const telegraphs = () => meshesOn(p, 'patch').filter((m) => m.visibility > 0).length;
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
        frames(p, 17, 17, [mob(1, 'king_slime', 6)]);
        expect(numbers(p)).toEqual([
          ['dot', '-2'],
          ['dot', '3'],
        ]);
        const shown = [...p.parent.querySelectorAll<HTMLElement>('[data-number]')];
        expect(shown.map((e) => e.style.color)).toEqual([
          rgb(statusColor('poison')),
          rgb(statusColor('burn')),
        ]);
        expect(nodesOn(p, 'spark')).toHaveLength(0);
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
      const balls = () => meshesOn(p, 'ball').filter((m) => m.isEnabled()).length;
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

    it('rims a boss’s area in its own colour and edges it in the colour of the status it inflicts, its unblockable slam red and its family attack yellow; a normal mob’s attack, drawing no area, shows neither', async () => {
      const p = await playing(FLAT, 4, 20);
      const pack = [mob(1, 'orange_mushroom', 7), mob(2, 'vampire_bat', 1)];
      frames(p, 2, 10, pack);
      const edges = () =>
        meshesOn(p, 'line')
          .filter((m) => m.isEnabled() && m.visibility > 0)
          .map((m) => (m.material as StandardMaterial).emissiveColor.toHexString().toLowerCase());
      const shown = () => meshesOn(p, 'patch').filter((m) => m.visibility > 0);
      const glows = () =>
        shown().map(
          (m) => (m.material as NodeMaterial).alphaMode === Constants.ALPHA_PREMULTIPLIED,
        );
      const hue = (hex: string) => {
        const c = Color3.FromHexString(hex);
        return c.g / c.r < 0.5 ? 'red' : 'yellow';
      };
      p.push(
        'mobAttacked',
        { mob: 1, attack: 0, yaw: -Math.PI / 2, toX: 4, toY: 0, toZ: 20 },
        { tick: 11 },
      );
      frames(p, 11, 16, pack);
      expect([edges(), shown()]).toEqual([[], []]); // the mushroom's poisoning charge: no area, so no edge
      frames(p, 17, 40, pack);
      p.push('mobAttacked', { mob: 2, attack: 1, yaw: Math.PI / 2 }, { tick: 41 });
      frames(p, 41, 46, pack);
      expect([hue(edges()[0]!), edges()[1]]).toEqual([
        'red',
        `#${statusColor('stun').toString(16).padStart(6, '0')}`,
      ]); // the slam's rim, then its stun outside it
      expect(glows()).toEqual([true, true]); // its area and its fill
      const red = (shown()[0]!.material as NodeMaterial).getBlockByName('color') as InputBlock;
      expect((red.value as Color3).g / (red.value as Color3).r).toBeLessThan(0.6); // the slam, red
      frames(p, 47, 80, pack);
      p.push(
        'mobAttacked',
        { mob: 2, attack: 0, yaw: Math.PI / 2, toX: 4, toY: 0, toZ: 20 },
        { tick: 81 },
      );
      frames(p, 81, 86, pack);
      expect(edges().map(hue)).toEqual(['yellow']); // its rim alone: its spit inflicts nothing
      const yellow = (shown()[0]!.material as NodeMaterial).getBlockByName('color') as InputBlock;
      expect((yellow.value as Color3).g / (yellow.value as Color3).r).toBeGreaterThan(0.75); // its spit, yellow
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
            key('KeyQ', true);
            key('KeyQ', false);
          }
          p.frames(1);
          const sent = p.ws.frames();
          const press = sent.find((f) => f.t === 'useSkill');
          const inputs = sent.filter((f) => f.t === 'input');
          while (ran < inputs.length - 2) {
            const f = inputs[ran++]!;
            if (press && f.seq === press.seq + 1) {
              server = dash(server, { x: 0, z: 1, length: 4 }, 4 * steps, 'stop', false, movement);
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
          { skill: 'strike', target: 'm:1', way: { x: 0, z: 1, length: 4 } },
        ]);
        const sentAt = p.ws.frames().find((f) => f.t === 'useSkill')!.seq;
        expect(sentAt > pressedAt + 1).toBe(attackAt >= 0); // mid-swing, sent at the cancel point
        expect(server.z).toBeCloseTo(24, 9); // dashed the skill's 4 m toward the lock
        expect(p.world.resets).toBe(0);
      } finally {
        realTimers();
        strike.move = was;
      }
    },
  );

  it('predicts Deflect’s leap from the input after its guard’s last, as the world starts it, with no correction (C21, from the live pass)', async () => {
    const p = await playing(FLAT, 10, 20, 'bowman');
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'bowman',
          level: 10,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: {} } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['deflect']);
    frames(p, 2, 10, []);
    const g = (await loadMap(FLAT)).geometry;
    const steps = tickTiming(RATE, movement.dt).stepsPerTick;
    let server: State = { x: 10, y: 0, z: 20, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 1, hz: 0 };
    let ran = 0;
    let tick = 10;
    let leaptAt = -1;
    let stoodTill = -1; // the last input the world stepped the player standing
    for (let i = 0; i < 40; i++) {
      if (i === 5) {
        key('KeyQ', true);
        key('KeyQ', false);
      }
      p.frames(1);
      const sent = p.ws.frames();
      const press = sent.find((f) => f.t === 'useSkill');
      const inputs = sent.filter((f) => f.t === 'input');
      while (ran < inputs.length - 2) {
        const f = inputs[ran++]!;
        if (press && f.seq === press.seq + 12 + 1) {
          const way = (press.d as { way: { x: number; z: number; length: number } }).way;
          server = dash(server, way, 6 * steps, 'stop', true, movement); // 300 ms
          leaptAt = f.seq;
        } else if (leaptAt < 0) {
          stoodTill = f.seq;
        }
        for (let k = 0; k < steps; k++) {
          server = step(server, f.d as unknown as Parameters<typeof step>[1], g, movement);
        }
        p.push('delta', { players: [wireOf(server)], ackSeq: f.seq }, { tick: ++tick });
      }
    }
    const press = p.ws.frames().find((f) => f.t === 'useSkill')!;
    const used = press.d as { skill: string; way: { x: number; z: number; length: number } };
    expect([used.skill, used.way.x, Math.abs(used.way.z), used.way.length]).toEqual([
      'deflect',
      -1,
      0,
      2,
    ]); // straight back from the heading east
    expect([leaptAt, stoodTill]).toEqual([press.seq + 13, press.seq + 12]); // 600 ms of guard: 12 inputs
    expect(server.x).toBeCloseTo(8, 9); // 2 m back
    expect(p.world.resets).toBe(0);
  });

  it('a stun 400 ms into Deflect’s guard drops its leap: none predicted at 600 ms nor once the stun ends, as the world drops it, correcting for the stun alone (C21, from the live pass)', async () => {
    const p = await playing(FLAT, 10, 20, 'bowman');
    character.getState().applyWelcome(
      create(WelcomeSchema, {
        token: 'tok-1',
        mapId: FLAT,
        character: {
          id: ME,
          name: 'Ayla',
          class: 'bowman',
          level: 10,
          hp: 50,
          maxHp: 50,
          sheet: { skills: { unspent: 0, invested: {} } },
        },
      }),
    );
    energy.getState().welcome(10);
    skillBar.getState().load(['deflect']);
    frames(p, 2, 10, []);
    const g = (await loadMap(FLAT)).geometry;
    const steps = tickTiming(RATE, movement.dt).stepsPerTick;
    let server: State = { x: 10, y: 0, z: 20, vx: 0, vy: 0, vz: 0, on_ground: true, hx: 1, hz: 0 };
    let ran = 0;
    let tick = 10;
    let stunEnds = 0;
    let furthest = 10; // the body's least x drawn: a leap goes 2 m west
    const resetAt: number[] = []; // inputs after the press whose frame the client hadn't predicted
    const body = () => p.root(classLook('bowman').file)!.position.x;
    for (let i = 0; i < 50; i++) {
      if (i === 5) {
        key('KeyQ', true);
        key('KeyQ', false);
      }
      p.frames(1);
      furthest = Math.min(furthest, body());
      const sent = p.ws.frames();
      const press = sent.find((f) => f.t === 'useSkill');
      const inputs = sent.filter((f) => f.t === 'input');
      while (ran < inputs.length - 2) {
        const f = inputs[ran++]!;
        if (press && f.seq === press.seq + 8) {
          server = { ...server, held: 20 * steps }; // the slam's 1 s stun, 8 inputs into the 12 of guard
          stunEnds = tick + 21;
        }
        for (let k = 0; k < steps; k++) {
          server = step(server, f.d as unknown as Parameters<typeof step>[1], g, movement);
        }
        const statuses =
          tick < stunEnds ? [create(StatusViewSchema, { id: 'stun', ends: stunEnds })] : [];
        const resets = p.world.resets;
        p.push(
          'delta',
          { players: [{ ...wireOf(server), statuses }], ackSeq: f.seq },
          { tick: ++tick },
        );
        if (press && p.world.resets > resets) {
          resetAt.push(f.seq - press.seq);
        }
      }
    }
    const press = p.ws.frames().find((f) => f.t === 'useSkill')!;
    expect((press.d as { skill: string }).skill).toBe('deflect');
    expect(ran).toBeGreaterThan(
      press.seq - p.ws.frames().find((f) => f.t === 'input')!.seq + 12 + 20 + 6,
    ); // past the stun and a leap's time
    expect([server.x, server.held ?? 0]).toEqual([10, 0]); // the world's: still where pressed, the stun over
    expect(furthest).toBeCloseTo(10, 9); // nor did the client draw a leap, even for a frame
    expect([resetAt, p.world.resets]).toEqual([[8], 1]); // the stun alone, which no client foresees; no leap to take back
  });

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
