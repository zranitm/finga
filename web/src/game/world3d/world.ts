// The world in 3D (3d-world.md section 10): a map's scene as the server sends it, the player predicted over it, the keys sent, and a new scene at each map change.
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';

import { maps } from '@/gamedata/maps';
import { mobs as mobData } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';
import { npcs as npcData } from '@/gamedata/npcs';
import { skillKeys } from '@/input/actions';
import type { Action } from '@/input/actions';
import { isTyping } from '@/input/typing';
import { FixedStep, ticksIn, tickTiming } from '@/lib/fixed-step';
import type { TickTiming } from '@/lib/fixed-step';
import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import { actionPoints } from '@/progression/actions';
import { markerFor, npcById, npcsOn } from '@/progression/quests';
import type { Npc } from '@/progression/quests';
import { bonusAt, moveSpeedWithout, skillById } from '@/progression/skills';
import type { Skill, Stage as SkillStage } from '@/progression/skills';
import { moveSpeedWith } from '@/progression/stats';
import { MobPose } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { State } from '@/sim/step';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { inventory } from '@/state/inventory';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { questPlayer, quests } from '@/state/quests';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

import { ActionClock, attackIn, attackTiming, skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';
import { Arrival } from '../systems/arrival';
import type { Held, Spot } from '../systems/arrival';
import { departure, DropTracker } from '../systems/drops';
import type { DropChanges, Point } from '../systems/drops';
import { FeedWatch } from '../systems/feed-watch';
import { HitStops } from '../systems/hit-stop';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import { MobTracker } from '../systems/mobs';
import type { MobState } from '../systems/mobs';
import { PotionKeys } from '../systems/potion-keys';
import { Predictor, movementParams, wireState } from '../systems/prediction';
import type { Pace } from '../systems/prediction';
import { SkillKeys } from '../systems/skill-keys';

import { beatsOf } from './beats';
import type { Beats } from './beats';
import type { Figure, Gait, Lighting, Pose } from './body';
import { STILL } from './body';
import { DropMark } from './drop';
import { Figures } from './figures';
import { cameraAt, easeShare, heldIn, turn, yawOf } from './follow';
import { Labels, MARKER_TEXT } from './labels';
import { classLook, mobLook, npcLook } from './looks';
import { groundUnder, loadMap, standsIn } from './map';
import type { MapSource, WorldMap } from './map';
import { Models } from './models';
import type { ModelSource } from './models';
import { buildPlace, fade } from './place';
import type { Place } from './place';
import { Palette } from './toon';

/** Who enters the world. */
export interface Player {
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
}

export interface WorldDeps {
  readonly engine: AbstractEngine;
  readonly socket: GameSocket;
  /** The page element the canvas fills, which labels and the fade lie over. */
  readonly parent: HTMLElement;
  readonly player: Player;
  readonly models: ModelSource;
  /** The maps' JSON, by default from the page's maps folder. */
  readonly maps?: MapSource;
  /** The page's clock in ms, which the keys, actions and frames share. */
  readonly now: () => number;
}

type Wire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];
type DropWire = Data<'snapshot'>['drops'][number];

const FADE_MS = 250;
const BANNER_MS = 1800;
const STALL_MS = 2000; // 40 ticks without a frame: the world has stalled, though the socket is open
const SUN = new Vector3(0.35, -1, 0.6);
const SHADOW_MAP = 2048;
const CHEST_M = 0.9; // the camera looks at the player's chest
const TURN_MS = 60; // a turn eases most of the way in this
const MAX_FRAME_MS = 100; // a frame after a stall moves no further than this
const RUN_SPEED = 3; // m/s: from here a body runs rather than walks
const STILL_SPEED = 0.1;
const DYING_MS = 1000; // a killed mob lies this long before it goes
const NAME_LIFT_M = 0.35; // a name over its head
const MARKER_LIFT_PX = 18; // a marker over its name
const PORTAL_LABEL_M = 2.2;
const CAMERA_FOV_HALF = 0.4; // Babylon's default field of view, 0.8 rad, halved

/** A figure drawn for one of the map's bodies, made in the background; its turn eases toward its facing. */
class Drawn {
  figure: Figure | null = null;
  yaw: number;
  private gone = false;
  private last: { x: number; y: number; z: number } | null = null;
  private posed: { p: Pose; at: number } = { p: 'up', at: 0 };

  constructor(made: Promise<Figure>, yaw: number) {
    this.yaw = yaw;
    void made.then((f) => {
      if (this.gone) {
        f.dispose();
        return;
      }
      this.figure = f;
      if (this.posed.p !== 'up') {
        f.pose(this.posed.p, this.posed.at); // taken while it loaded
      }
    });
  }

  /** Draws it at (x, y, z) turned toward yaw, its gait by gait, or else by how far it moved since the last frame. */
  draw(x: number, y: number, z: number, yaw: number, now: number, dtMs: number, gait?: Gait): void {
    const moved = this.last ? Math.hypot(x - this.last.x, z - this.last.z) : 0;
    this.last = { x, y, z };
    this.yaw = turn(this.yaw, yaw, Math.min(1, dtMs / TURN_MS));
    const f = this.figure;
    if (!f) {
      return;
    }
    f.root.position.set(x, y, z);
    f.root.rotation.y = this.yaw;
    f.update(now, dtMs, gait ?? gaitAt(dtMs > 0 ? (moved * 1000) / dtMs : 0, true));
  }

  act(kind: 'attack' | 'skill', beats: Beats): void {
    this.figure?.act(kind, beats);
  }

  /** Takes pose p from at, once. */
  pose(p: Pose, at: number): void {
    if (p !== this.posed.p) {
      this.posed = { p, at };
      this.figure?.pose(p, at);
    }
  }

  get at(): Point | undefined {
    return this.last ?? undefined;
  }

  dispose(): void {
    this.gone = true;
    this.figure?.dispose();
    this.figure = null;
  }
}

/** A body moving at speed m/s: still, walking or running. */
function gaitAt(speed: number, onGround: boolean): Gait {
  return {
    pace: speed < STILL_SPEED ? 'still' : speed < RUN_SPEED ? 'walk' : 'run',
    onGround,
  };
}

/** A mob's pose on the wire as a body takes it. */
function poseOf(p: MobPose): Pose {
  switch (p) {
    case MobPose.FLINCH:
      return 'flinch';
    case MobPose.DOWN:
      return 'down';
    case MobPose.RISING:
      return 'rising';
    default:
      return 'up';
  }
}

export class World {
  private scene: Scene | null = null;
  private camera: TargetCamera | null = null;
  private figures: Figures | null = null;
  private lighting: Lighting | null = null;
  private place: Place | null = null;
  private map: WorldMap | null = null;
  private readonly labels: Labels;
  private readonly veil: HTMLDivElement;
  private visit = 0; // each build and teardown starts the next; a build that finishes for an old one is thrown away
  private arriving: Arrival | null = null;
  private fading: ReturnType<typeof setTimeout> | null = null;
  private offs: (() => void)[] = [];
  private keysOff: (() => void) | null = null;

  private mapId = '';
  private tickRateHz = 20;
  private timing: TickTiming = tickTiming(20, movement.dt);
  private clock = new FixedStep(this.timing);
  private renderClock = new RenderClock(this.timing.tickMs);
  private lastTick = 0;
  private feed = new FeedWatch(STALL_MS);
  private stalled = false;
  private placed = false; // set by the first frame that names the local player
  private transitionState: TransitionState = initialTransition;

  private predictor: Predictor | null = null;
  private before: State | null = null; // the step before the predictor's last, the player drawn between them
  private sender: InputSender | null = null;
  private actions: ActionClock | null = null;
  private attackHeld = false;
  private myBeats: Beats | null = null;
  private readonly hitStops = new HitStops();
  private potionKeys: PotionKeys | null = null;
  private skillKeys: SkillKeys | null = null;

  private me: Drawn | null = null;
  private meCls = '';
  private readonly follow = new Vector3();
  private remotes: RemotePlayers | null = null;
  private readonly remoteViews = new Map<string, Drawn>();
  private readonly deadRemotes = new Set<string>();
  private mobs = new MobTracker(this.timing.tickMs);
  private readonly mobViews = new Map<number, Drawn>();
  private readonly dying: { readonly view: Drawn; readonly until: number }[] = [];
  private mobAttacks: { readonly mob: number; readonly at: number; readonly attack: number }[] = [];
  private readonly npcViews = new Map<
    string,
    { readonly view: Drawn; readonly npc: Npc; readonly y: number }
  >();
  private readonly drops = new DropTracker();
  private readonly dropViews = new Map<number, DropMark>();
  private readonly leaving: DropMark[] = [];

  private constructor(private readonly deps: WorldDeps) {
    this.labels = new Labels(deps.parent);
    this.veil = document.createElement('div');
    this.veil.style.cssText = `position:absolute;inset:0;background:#0a0c16;opacity:1;pointer-events:none;transition:opacity ${FADE_MS}ms`;
    deps.parent.appendChild(this.veil);
  }

  /** Enters the world on the map arriving holds, built once its map and models are in. */
  static async open(deps: WorldDeps, arriving: Arrival): Promise<World> {
    const w = new World(deps);
    w.bindKeys();
    await w.build(arriving);
    return w;
  }

  /** The map's scene, null while one is being built. */
  get current(): Scene | null {
    return this.scene;
  }

  /** One frame of dtMs: the player stepped and sent at the world's tick, every body drawn, the camera eased after the player, then rendered. */
  frame(dtMs: number): void {
    const scene = this.scene;
    const predictor = this.predictor;
    const sender = this.sender;
    if (!scene || !predictor || !sender) {
      return;
    }
    const dt = Math.min(dtMs, MAX_FRAME_MS);
    const now = this.deps.now();
    this.setStalled(this.feed.stalled(now));
    if (this.placed) {
      if (this.attackHeld) {
        this.attack(false);
      }
      for (const step of this.clock.advance(dt)) {
        if (this.transitionState.dead || this.stalled) {
          continue; // the server holds a dead player; a stalled world would take no input, so none is predicted
        }
        if (step.tickStart) {
          const view = this.renderClock.viewAt(now);
          predictor.beginTick(this.deps.socket.sequence, sender.tickStart(view), view);
        }
        this.before = predictor.state;
        predictor.subStep();
      }
    }
    this.drawMe(now, dt);
    this.drawRemotes(now, dt);
    this.drawMobs(now, dt);
    this.drawNpcs(now, dt);
    this.drawDrops(now);
    this.moveCamera(dt);
    this.closeDialogOutOfReach();
    skillBar.getState().endBuffs(now);
    if (this.camera) {
      this.labels.place(scene, this.camera);
    }
    scene.render();
  }

  dispose(): void {
    this.teardown();
    this.keysOff?.();
    this.keysOff = null;
    this.labels.dispose();
    this.veil.remove();
  }

  /** Builds the map arriving names, then applies what it held; a teardown or another build meanwhile throws it away. */
  private async build(arriving: Arrival): Promise<void> {
    const visit = ++this.visit;
    this.arriving = arriving;
    const map = await loadMap(arriving.mapId, this.deps.maps);
    if (visit !== this.visit) {
      return;
    }
    const scene = new Scene(this.deps.engine);
    const sun = new DirectionalLight('sun', SUN.normalizeToNew(), scene);
    const shadows = new ShadowGenerator(SHADOW_MAP, sun);
    shadows.usePercentageCloserFiltering = true;
    const lighting: Lighting = { palette: new Palette(scene), shadows };
    const models = new Models(scene, this.deps.models);
    const figures = new Figures(scene, lighting, models);
    const place = await buildPlace(scene, map, lighting, models);
    if (visit !== this.visit) {
      scene.dispose();
      return;
    }
    this.scene = scene;
    this.lighting = lighting;
    this.figures = figures;
    this.place = place;
    this.map = map;
    this.camera = new TargetCamera('camera', Vector3.Zero(), scene);
    this.start(arriving);
  }

  /** Starts the built map: its clocks and trackers, the player hidden until their frame, the NPCs and portal labels, and the socket's frames. */
  private start(arriving: Arrival): void {
    const map = this.map!;
    const socket = this.deps.socket;
    const { characterId } = this.deps.player;
    this.mapId = map.id;
    this.tickRateHz = arriving.tickRateHz;
    this.timing = tickTiming(this.tickRateHz, movement.dt);
    this.clock = new FixedStep(this.timing);
    this.renderClock = new RenderClock(this.timing.tickMs);
    this.mobs = new MobTracker(this.timing.tickMs);
    this.feed = new FeedWatch(STALL_MS);
    this.remotes = new RemotePlayers(characterId, this.timing.tickMs);
    this.actions = new ActionClock(this.timing.tickMs, this.deps.now, () => undefined);
    this.predictor = new Predictor(
      map.geometry,
      this.timing,
      movementParams(this.kind(), this.moveSpeed()),
      (view) => this.mobs.blockersAt(view),
    );
    this.sender = new InputSender({
      send: (input, tick, view) => socket.sendInput(input, tick, view),
    });
    const at = arriving.at;
    this.meCls = this.cls;
    this.me = this.drawn(classLook(this.meCls), at?.yaw ?? 0);
    if (at) {
      this.follow.set(at.x, at.y + CHEST_M, at.z);
    }
    this.potionKeys = this.newPotionKeys();
    this.skillKeys = this.newSkillKeys();
    for (const npc of npcsOn(map.id)) {
      const y = groundUnder(map, npc.x, npc.z);
      this.npcViews.set(npc.id, { view: this.drawn(npcLook(npc.look), npc.yaw), npc, y });
    }
    this.showMarkers();
    for (const p of map.portals) {
      const name = maps.find((m) => m.id === p.to)?.name ?? p.to;
      this.labels.set(
        `portal:${p.name}`,
        'portal',
        `To ${name}`,
        p.x,
        groundUnder(map, p.x, p.z) + PORTAL_LABEL_M,
        p.z,
      );
    }
    this.banner(map.id);
    this.offs.push(
      quests.subscribe((now, before) => {
        if (now.log !== before.log) {
          this.showMarkers();
        }
      }),
      character.subscribe((now, before) => {
        if (now.character?.level !== before.character?.level) {
          this.showMarkers();
        }
        if (now.character && now.character.class !== this.meCls) {
          this.meCls = now.character.class;
          const yaw = this.me?.yaw ?? 0;
          this.me?.dispose();
          this.me = this.drawn(classLook(this.meCls), yaw);
        }
        if (now.character?.sheet !== before.character?.sheet) {
          this.predictor?.setParams(movementParams(this.kind(), this.moveSpeed()));
        }
      }),
      inventory.subscribe((now, before) => {
        if (now.slots !== before.slots) {
          this.showMarkers();
        }
      }),
      socket.on('snapshot', (m) => this.onWorld(m)),
      socket.on('delta', (m) => this.onWorld(m)),
      socket.on('died', (m) => this.onWorld(m)),
      socket.on('respawned', (m) => this.onWorld(m)),
      socket.on('welcome', (m) => this.onWelcome(m.d.mapId, m.d.tickRateHz)),
      socket.on('mapChange', (m) => this.onTransition(m)),
      socket.on('damage', (m) => this.onDamage(m.d)),
      socket.on('attackUsed', (m) => this.onAttackUsed(m.d)),
      socket.on('skillUsed', (m) => this.onSkillUsed(m.d)),
      socket.on('mobAttacked', (m) =>
        this.mobAttacks.push({ mob: m.d.mob, attack: m.d.attack, at: m.tick * this.timing.tickMs }),
      ),
    );
    for (const m of arriving.take()) {
      this.onWorld(m);
    }
    this.arriving = null;
    this.veil.style.opacity = '0';
  }

  private bindKeys(): void {
    this.keysOff = bindKeyboard({
      target: {
        press: (a, again) => {
          if (a === 'attack') {
            this.attackHeld = true;
            if (!again) {
              this.attack(true); // a key held again after a release-all repeats as a held key does
            }
            return;
          }
          this.sender?.press(a);
        },
        release: (a) => {
          if (a === 'attack') {
            this.attackHeld = false;
            return;
          }
          this.sender?.release(a);
        },
        releaseAll: () => {
          this.attackHeld = false;
          this.sender?.releaseAll();
        },
      },
      preset: () => settings.getState().preset,
      typing: isTyping,
      onAction: (action) => this.onAction(action),
    });
  }

  private onAction(action: Action): void {
    const key = (skillKeys as readonly Action[]).indexOf(action);
    if (key >= 0) {
      if (this.free()) {
        this.skillKeys?.press(key);
      }
      return;
    }
    switch (action) {
      case 'inventory':
      case 'character':
      case 'skills':
      case 'party':
      case 'quests':
        ui.getState().toggleWindow(action);
        return;
      case 'talk':
        this.talk();
        return;
      case 'hpPotion':
        if (this.free()) {
          this.potionKeys?.press();
        }
        return;
      case 'chat':
        ui.getState().openChat();
        return;
      case 'close':
        ui.getState().closeTopWindow();
        return;
      default:
        return;
    }
  }

  /** Whether the player may act: placed, the world not stalled, alive. */
  private free(): boolean {
    return this.placed && !this.stalled && !this.transitionState.dead;
  }

  private onWorld(m: Held): void {
    this.feed.heard();
    switch (m.t) {
      case 'snapshot':
        this.onSnapshot(m.d.players, m.d.mobs, m.d.drops, this.tickOf(m.tick));
        return;
      case 'delta':
        this.onDelta(m.d, this.tickOf(m.tick));
        return;
      case 'died':
        this.onTransition({ t: 'died', d: m.d });
        return;
      case 'respawned':
        this.onTransition({ t: 'respawned', d: m.d });
        return;
    }
  }

  /** proto3 sends no tick as 0. */
  private tickOf(tick: number): number {
    this.lastTick = tick === 0 ? this.lastTick + 1 : tick;
    return this.lastTick;
  }

  private onSnapshot(
    players: readonly Wire[],
    mobs: readonly MobWire[],
    drops: readonly DropWire[],
    tick: number,
  ): void {
    const now = this.deps.now();
    const t = tick * this.timing.tickMs;
    this.renderClock.update(tick, now);
    this.remotes?.applySnapshot(t, players);
    this.mobs.applySnapshot(t, mobs);
    this.applyDrops(this.drops.applySnapshot(drops), now);
    this.deadRemotes.clear();
    for (const p of players) {
      if (p.dead && p.id !== this.deps.player.characterId) {
        this.deadRemotes.add(p.id);
      }
    }
    const me = players.find((p) => p.id === this.deps.player.characterId);
    if (me) {
      this.placeAt(me);
    }
  }

  private onDelta(d: Data<'delta'>, tick: number): void {
    const now = this.deps.now();
    const t = tick * this.timing.tickMs;
    this.renderClock.update(tick, now);
    this.remotes?.applyDelta(t, d.players, d.removedPlayers);
    this.mobs.applyDelta(t, d.mobs, d.removedMobs);
    this.applyDrops(this.drops.applyDelta(d.drops, d.removedDrops, d.collectedDrops), now);
    for (const p of d.players) {
      if (p.id === this.deps.player.characterId) {
        continue;
      }
      if (p.dead) {
        this.deadRemotes.add(p.id);
      } else {
        this.deadRemotes.delete(p.id);
      }
    }
    for (const id of d.removedPlayers) {
      this.deadRemotes.delete(id);
    }
    const me = d.players.find((p) => p.id === this.deps.player.characterId);
    if (!me || !this.predictor) {
      return;
    }
    if (!this.placed) {
      this.placeAt(me); // after a respawn here, the first frame places them
      return;
    }
    if (!this.transitionState.dead) {
      this.predictor.reconcile(wireState(me), d.ackSeq);
    }
    character.getState().setHp(me.hp);
  }

  /** Sets the prediction on the server's state of the player, and shows them. */
  private placeAt(me: Wire): void {
    if (!this.predictor) {
      return;
    }
    this.predictor.reset(wireState(me));
    this.before = this.predictor.state;
    if (!this.placed) {
      this.follow.set(me.x, me.y + CHEST_M, me.z);
      if (this.me) {
        this.me.yaw = yawOf(me.hx, me.hz);
      }
    }
    this.placed = true;
    character.getState().setHp(me.hp);
  }

  private applyDrops({ added, removed, collected }: DropChanges, now: number): void {
    const lighting = this.lighting;
    const scene = this.scene;
    if (!lighting || !scene) {
      return;
    }
    for (const d of added) {
      this.dropViews.set(
        d.id,
        new DropMark(scene, d.template, { x: d.x, y: d.y, z: d.z }, lighting),
      );
    }
    for (const d of [...removed, ...collected]) {
      const v = this.dropViews.get(d.id);
      if (!v) {
        continue;
      }
      this.dropViews.delete(d.id);
      const how = departure(collected.includes(d), this.middleOf(d.ownerId));
      v.leave(now, how.kind === 'fly' ? () => this.middleOf(d.ownerId) ?? how.to : null);
      this.leaving.push(v);
    }
  }

  /** The middle of a player drawn on this map, undefined when they aren't. */
  private middleOf(id: string): Point | undefined {
    const at =
      id === this.deps.player.characterId ? this.predictor?.state : this.remoteViews.get(id)?.at;
    return at && { x: at.x, y: at.y + movement.height / 2, z: at.z };
  }

  private drawMe(now: number, dtMs: number): void {
    const me = this.me;
    const p = this.predictor;
    if (!me || !p || !this.placed) {
      return;
    }
    const a = this.before ?? p.state;
    const b = p.state;
    const f = this.clock.fraction;
    const speed = Math.hypot(b.vx, b.vz);
    me.draw(
      a.x + (b.x - a.x) * f,
      a.y + (b.y - a.y) * f,
      a.z + (b.z - a.z) * f,
      yawOf(b.hx, b.hz),
      now,
      dtMs,
      gaitAt(speed, b.on_ground),
    );
    me.pose(this.transitionState.dead ? 'dead' : 'up', now);
    this.labels.set(
      'me',
      'self',
      this.deps.player.name,
      b.x,
      b.y + movement.height + NAME_LIFT_M,
      b.z,
    );
  }

  private drawRemotes(now: number, dtMs: number): void {
    const remotes = this.remotes;
    const renderTime = this.renderClock.renderTime(now);
    if (!remotes || renderTime === null) {
      return;
    }
    const { shown, gone } = remotes.render(renderTime);
    let changed = gone.length > 0;
    for (const id of gone) {
      this.remoteViews.get(id)?.dispose();
      this.remoteViews.delete(id);
      this.labels.delete(`p:${id}`);
    }
    for (const [id, s] of shown) {
      let v = this.remoteViews.get(id);
      if (!v) {
        v = this.drawn(classLook(remotes.classOf(id)), yawOf(s.hx, s.hz));
        this.remoteViews.set(id, v);
        changed = true;
      }
      v.draw(
        s.x,
        s.y,
        s.z,
        yawOf(s.hx, s.hz),
        now,
        dtMs,
        gaitAt(Math.hypot(s.vx, s.vz), s.on_ground),
      );
      v.pose(this.deadRemotes.has(id) ? 'dead' : 'up', now);
      this.labels.set(
        `p:${id}`,
        'player',
        remotes.nameOf(id),
        s.x,
        s.y + movement.height + NAME_LIFT_M,
        s.z,
      );
    }
    if (changed) {
      nearby
        .getState()
        .setNearby([...this.remoteViews.keys()].map((id) => ({ id, name: remotes.nameOf(id) })));
    }
  }

  private drawMobs(now: number, dtMs: number): void {
    const renderTime = this.renderClock.renderTime(now);
    if (renderTime === null) {
      return;
    }
    const { shown, gone } = this.mobs.render(renderTime);
    for (const id of gone) {
      const v = this.mobViews.get(id);
      this.mobViews.delete(id);
      if (v) {
        v.pose('dead', now);
        this.dying.push({ view: v, until: now + DYING_MS });
      }
    }
    for (let i = this.dying.length - 1; i >= 0; i--) {
      const d = this.dying[i]!;
      if (now >= d.until) {
        d.view.dispose();
        this.dying.splice(i, 1);
      } else {
        const at = d.view.at;
        if (at) {
          d.view.draw(at.x, at.y, at.z, d.view.yaw, now, dtMs, STILL);
        }
      }
    }
    for (const [id, s] of shown) {
      let v = this.mobViews.get(id);
      if (!v) {
        v = this.drawn(mobLook(s.type), s.yaw);
        this.mobViews.set(id, v);
      }
      v.draw(s.x, s.y, s.z, s.yaw, now, dtMs);
      v.pose(poseOf(s.pose), now - s.poseMs);
    }
    this.startMobAttacks(renderTime, now, shown);
    const boss = fieldBoss.getState().boss;
    const drawn = boss && shown.get(boss.mobId);
    fieldBoss.getState().setHp(drawn ? { hp: drawn.hp, maxHp: drawn.maxHp } : null); // the boss bar shows what is drawn
  }

  /** Each mob attack whose windup the drawn frames have reached plays its clip: the strike at the windup's end, ready after its travel and recover. */
  private startMobAttacks(
    renderTime: number,
    now: number,
    shown: ReadonlyMap<number, MobState>,
  ): void {
    const due = this.mobAttacks.filter((a) => a.at <= renderTime);
    if (due.length === 0) {
      return;
    }
    this.mobAttacks = this.mobAttacks.filter((a) => a.at > renderTime);
    for (const a of due) {
      const v = this.mobViews.get(a.mob);
      const type = shown.get(a.mob)?.type;
      const attack = mobData.find((m) => m.id === type)?.attacks[a.attack];
      if (!v || !attack) {
        continue;
      }
      const start = now - (renderTime - a.at);
      const hit = start + attack.windup_ms;
      const readyAt = hit + attack.travel_ms + attack.recover_ms;
      v.act('attack', { start, hit, strikeEnd: hit + attack.travel_ms, readyAt, end: readyAt });
    }
  }

  private drawNpcs(now: number, dtMs: number): void {
    for (const { view, npc, y } of this.npcViews.values()) {
      view.draw(npc.x, y, npc.z, npc.yaw, now, dtMs, STILL);
    }
  }

  private drawDrops(now: number): void {
    for (const v of this.dropViews.values()) {
      v.update(now);
    }
    for (let i = this.leaving.length - 1; i >= 0; i--) {
      if (!this.leaving[i]!.update(now)) {
        this.leaving.splice(i, 1);
      }
    }
  }

  /** Eases the camera toward the player's chest by the time passed, held so the map's edge stops it, and fades what stands between them. */
  private moveCamera(dtMs: number): void {
    const camera = this.camera;
    const map = this.map;
    const at = this.me?.at;
    if (!camera || !map || !at || !this.placed) {
      return;
    }
    const share = easeShare(dtMs);
    this.follow.x += (at.x - this.follow.x) * share;
    this.follow.y += (at.y + CHEST_M - this.follow.y) * share;
    this.follow.z += (at.z - this.follow.z) * share;
    const engine = this.deps.engine;
    const aspect =
      engine.getRenderHeight() > 0 ? engine.getRenderWidth() / engine.getRenderHeight() : 16 / 9;
    const halfDepth = cameraAt(0, 0, 0)[1] * Math.tan(CAMERA_FOV_HALF);
    const x = heldIn(this.follow.x, map.geometry.size_x, halfDepth * aspect);
    const z = heldIn(this.follow.z, map.geometry.size_z, halfDepth);
    const [cx, cy, cz] = cameraAt(x, this.follow.y, z);
    camera.position.set(cx, cy, cz);
    camera.setTarget(new Vector3(x, this.follow.y, z));
    if (this.place) {
      fade(camera.position, new Vector3(at.x, at.y + movement.height, at.z), this.place.solids);
    }
  }

  private drawn(look: Parameters<Figures['make']>[0], yaw: number): Drawn {
    const figures = this.figures;
    const made = figures ? figures.make(look) : Promise.reject(new Error('no scene'));
    made.catch(() => undefined);
    return new Drawn(made, yaw);
  }

  /** The player's class: the store's, which a promotion changes, else the one the world was entered with. */
  private get cls(): string {
    return character.getState().character?.class ?? this.deps.player.cls;
  }

  private kind(): 'town' | 'hunting' {
    return maps.find((m) => m.id === this.mapId)?.kind === 'hunting' ? 'hunting' : 'town';
  }

  private moveSpeed(): number {
    return character.getState().character?.sheet?.derived?.moveSpeed ?? 0;
  }

  private setStalled(stalled: boolean): void {
    if (stalled !== this.stalled) {
      this.stalled = stalled;
      connection.getState().setWorldStalled(stalled);
    }
  }

  /** The yaw an action aims along: the player's heading. */
  private heading(): number {
    const s = this.predictor?.state;
    return s ? yawOf(s.hx, s.hz) : 0;
  }

  /** A fresh press or a held key attacks as soon as an attack may start: at a cancelable skill's cancel point, or as the running action ends; a held key never goes ahead of a press waiting for its moment. */
  private attack(fresh: boolean): void {
    const clock = this.actions;
    if (this.kind() === 'town' || !this.free() || !clock) {
      return; // the server runs no combat on town maps
    }
    if (fresh) {
      if (!clock.press('attack', () => this.attack(true))) {
        return;
      }
    } else if (clock.waiting()) {
      return;
    } else if (!clock.canStart('attack')) {
      clock.repeatWhenFree(() => this.attackHeld && this.attack(false));
      return;
    }
    if (!this.deps.socket.sendAction('attack', { aim: this.heading() })) {
      return;
    }
    const timing = attackTiming(
      character.getState().character?.sheet,
      this.cls,
      this.timing.tickMs,
    );
    clock.begin(timing);
    this.act(timing, 'attack');
  }

  /** The player's own action drawn from now over its beats. */
  private act(timing: ActionTiming, kind: 'attack' | 'skill'): Beats {
    const beats = beatsOf(timing, this.deps.now(), this.timing.tickMs);
    this.myBeats = beats;
    this.me?.act(kind, beats);
    return beats;
  }

  /** Another player's basic attack, drawn over its length; the player's own, already drawn, says the world counted it. */
  private onAttackUsed(d: Data<'attackUsed'>): void {
    if (d.attacker === this.deps.player.characterId) {
      this.actions?.counted();
      return;
    }
    const cls = this.remotes?.classOf(d.attacker);
    if (cls) {
      this.remoteViews
        .get(d.attacker)
        ?.act('attack', beatsOf(attackIn(cls, d.lengthTicks), this.deps.now(), this.timing.tickMs));
    }
  }

  /** Another player's skill, drawn from its arrival; the player's own buff is timed here for the character window. */
  private onSkillUsed(d: Data<'skillUsed'>): void {
    const skill = skillById(d.skill);
    const stage = skill?.stages[d.stage];
    if (!skill || !stage) {
      return;
    }
    if (d.caster !== this.deps.player.characterId) {
      this.remoteViews
        .get(d.caster)
        ?.act(
          'skill',
          beatsOf(skillTiming(stage, this.timing.tickMs), this.deps.now(), this.timing.tickMs),
        );
      return;
    }
    if (skill.effect === 'buff') {
      const points = character.getState().character?.sheet?.skills?.invested[d.skill] ?? 0;
      skillBar
        .getState()
        .buff({ skill: d.skill, points, until: this.deps.now() + stage.duration_ms });
    }
  }

  /** A hit the player landed holds their pose and the mob's a moment; the HP comes with the frames. */
  private onDamage(d: Data<'damage'>): void {
    if (d.source !== `p:${this.deps.player.characterId}` || d.miss || !this.myBeats) {
      return;
    }
    const now = this.deps.now();
    const held = this.hitStops.hit(now, this.myBeats.readyAt);
    if (!held) {
      return;
    }
    this.me?.figure?.hold(held);
    const mob = d.target.startsWith('m:')
      ? this.mobViews.get(Number(d.target.slice(2)))
      : undefined;
    mob?.figure?.hold({ from: now, until: held.until, readyAt: held.until });
  }

  /** A buff that speeds the player paces the prediction as the world moves them: faster from the input after its hit, back from the one after its end. */
  private buffPaces(skill: Skill, stage: SkillStage, timing: ActionTiming): readonly Pace[] {
    const c = character.getState().character;
    const bonus =
      skill.effect === 'buff'
        ? (bonusAt(stage.bonus, c?.sheet?.skills?.invested[skill.id] ?? 0).moveSpeed ?? 0)
        : 0;
    const predictor = this.predictor;
    if (bonus <= 0 || !predictor) {
      return [];
    }
    const kind = this.kind();
    const fast = movementParams(kind, moveSpeedWith(c?.sheet?.derived?.moveSpeed ?? 0, bonus));
    const hit = this.deps.socket.sequence + actionPoints(timing.ticks, stage.hit, stage.cancel).hit;
    return [
      predictor.pace(hit, () => fast),
      predictor.pace(hit + ticksIn(stage.duration_ms, this.timing.tickMs), () => {
        const now = character.getState().character;
        const sheet = {
          moveSpeed: now?.sheet?.derived?.moveSpeed ?? 0,
          skillsMoveSpeed: now?.sheet?.derivedSkills?.moveSpeed ?? 0,
        };
        const invested = now?.sheet?.skills?.invested ?? {};
        return movementParams(
          kind,
          moveSpeedWithout(sheet, now?.class ?? '', invested, skillBar.getState().buffs, skill.id),
        );
      }),
    ];
  }

  /** The player's own skill starts as sent: its action, beats and pace; the undo, for a refusal, takes them back. */
  private beginSkill(skill: Skill, stage: SkillStage): () => void {
    const timing = skillTiming(stage, this.timing.tickMs);
    const giveBack = this.actions?.begin(timing) ?? (() => undefined);
    const paces = this.buffPaces(skill, stage, timing);
    this.act(timing, 'skill');
    return () => {
      giveBack();
      for (const m of paces.toReversed()) {
        this.predictor?.dropPace(m); // the last applied first, so each gives back the pace before it
      }
    };
  }

  private newSkillKeys(): SkillKeys {
    const socket = this.deps.socket;
    return new SkillKeys({
      slots: () => skillBar.getState().slots,
      caster: () => {
        const c = character.getState().character;
        const p = this.predictor?.state;
        return c
          ? {
              invested: c.sheet?.skills?.invested ?? {},
              energy: energy.getState().shown,
              cooldownSpeed: c.sheet?.derived?.cooldownSpeed ?? 0,
              dead: this.transitionState.dead,
              hurt: c.hp < c.maxHp,
              feet: this.placed && p ? { x: p.x, y: p.y, z: p.z } : null,
              hurtAllies: this.hurtAllies(c.id).map((a) => a.at),
            }
          : null;
      },
      inTown: () => this.kind() === 'town',
      tickMs: this.timing.tickMs,
      now: this.deps.now,
      cooldowns: () => skillBar.getState().cooldowns,
      cool: (skill, cooldown) => skillBar.getState().cool(skill, cooldown),
      use: (skill) => socket.request('useSkill', { skill, aim: this.heading() }, socket.sequence),
      notice: (text) => toasts.getState().push(text),
      free: (again) => this.actions?.press('skill', again) ?? false,
      awaitCount: (again) => this.actions?.awaitCount(again) ?? false,
      begin: (skill, stage) => this.beginSkill(skill, stage),
      spend: (cost) => energy.getState().spend(socket.sequence, cost),
    });
  }

  private newPotionKeys(): PotionKeys {
    return new PotionKeys({
      vitals: () => character.getState().character,
      useTab: () => inventory.getState().slots.use,
      use: (potion) =>
        this.deps.socket.request('useItem', {
          idempotencyKey: idempotencyKey(),
          itemId: potion.itemId,
          template: potion.template,
        }),
      drank: (template, hp) => combatLog.getState().drank(template, hp),
      notice: (text) => toasts.getState().push(text),
    });
  }

  /** The party members alive on this map below full HP, but for self, and where each is drawn, null for one not yet. */
  private hurtAllies(self: string): { id: string; at: Point | null }[] {
    return (party.getState().party?.members ?? [])
      .filter(
        (m) =>
          m.characterId !== self &&
          m.online &&
          m.mapId === this.mapId &&
          m.hp > 0 &&
          m.hp < m.maxHp,
      )
      .map((m) => ({ id: m.characterId, at: this.remoteViews.get(m.characterId)?.at ?? null }));
  }

  /** The talk key enters the portal the player stands in, else talks to the nearest NPC within reach. */
  private talk(): void {
    const p = this.predictor?.state;
    const map = this.map;
    if (!p || !map || !this.free()) {
      return;
    }
    if (standsIn(p, map)) {
      this.deps.socket.sendAction('enterPortal', {});
      return;
    }
    const npc = this.nearestInReach(p);
    if (!npc) {
      return;
    }
    this.deps.socket.request('talk', { npcId: npc.id }).catch((err: unknown) => {
      if (err instanceof RequestError && err.code !== 'disconnected' && err.code !== 'timeout') {
        toasts.getState().push(err.message);
      }
    });
  }

  /** How far the player's middle is from an NPC's point, as the server measures talk reach. */
  private reachTo(npc: Npc, p: State): number {
    const ground = this.map ? groundUnder(this.map, npc.x, npc.z) : 0;
    return Math.hypot(Math.hypot(npc.x - p.x, npc.z - p.z), p.y + movement.height / 2 - ground);
  }

  private nearestInReach(p: State): Npc | undefined {
    return npcsOn(this.mapId)
      .map((npc) => ({ npc, d: this.reachTo(npc, p) }))
      .filter((n) => n.d <= npcData.talk_reach)
      .sort((a, b) => a.d - b.d)[0]?.npc;
  }

  /** The NPC's dialog closes once the player dies, walks out of its reach, or leaves its map. */
  private closeDialogOutOfReach(): void {
    if (!ui.getState().windows.includes('npc')) {
      return;
    }
    const dialog = quests.getState().dialog;
    const npc = dialog ? npcById(dialog.npcId) : undefined;
    const p = this.predictor?.state;
    if (
      !npc ||
      !p ||
      npc.map !== this.mapId ||
      this.reachTo(npc, p) > npcData.talk_reach ||
      this.transitionState.dead
    ) {
      ui.getState().closeWindow('npc');
    }
  }

  /** Each NPC's name, and over it the marker of what it has for the player. */
  private showMarkers(): void {
    const player = questPlayer();
    for (const { npc, y } of this.npcViews.values()) {
      const id = npc.id;
      const top = y + movement.height + NAME_LIFT_M;
      this.labels.set(`npc:${id}`, 'npc', npc.name, npc.x, top, npc.z);
      const marker = markerFor(npc, player);
      this.labels.set(
        `mark:${id}`,
        'marker',
        marker ? MARKER_TEXT[marker] : '',
        npc.x,
        top,
        npc.z,
        MARKER_LIFT_PX,
      );
    }
  }

  private banner(mapId: string): void {
    const m = maps.find((x) => x.id === mapId);
    if (!m) {
      return;
    }
    const el = document.createElement('div');
    el.style.cssText = `position:absolute;left:0;right:0;top:28px;text-align:center;color:#f4f4f8;font:600 22px sans-serif;text-shadow:0 0 4px #0a0c16;pointer-events:none;transition:opacity ${2 * FADE_MS}ms`;
    el.textContent = m.min_level !== null ? `${m.name} · Lv ${m.min_level}–${m.max_level}` : m.name;
    this.deps.parent.appendChild(el);
    setTimeout(() => {
      el.style.opacity = '0';
      setTimeout(() => el.remove(), 2 * FADE_MS);
    }, BANNER_MS);
  }

  /** After a reconnect or a rejoin the server made: the same map waits for the next Snapshot; another map, or tick rate, builds anew. */
  private onWelcome(mapId: string, tickRateHz: number): void {
    this.feed.heard();
    if (mapId !== this.mapId || tickRateHz !== this.tickRateHz) {
      this.restartOn(mapId, tickRateHz, false);
      return;
    }
    this.transitionState = initialTransition;
    this.actions?.reset();
    this.placed = false;
    this.clearOthers();
  }

  private onTransition(msg: TransitionMsg): void {
    const r = transition(this.transitionState, msg, this.deps.player.characterId, this.mapId);
    this.transitionState = r.state;
    for (const a of r.actions) {
      this.applyTransition(a);
    }
  }

  private applyTransition(a: TransitionAction): void {
    switch (a.type) {
      case 'die':
        this.actions?.reset();
        skillBar.getState().endBuffs(); // the world ends them at death
        character.getState().setDead(true);
        ui.getState().closeWindow('npc');
        return;
      case 'respawn-here':
        this.placed = false;
        character.getState().setHp(a.hp);
        character.getState().setDead(false);
        return;
      case 'change-map':
        character.getState().setDead(false);
        this.restartOn(a.mapId, this.tickRateHz, true, a.at);
        return;
    }
  }

  /** From now on an Arrival holds the next map's messages; the old map goes once the veil is down, and the new one builds from what it held. */
  private restartOn(mapId: string, tickRateHz: number, fade: boolean, at?: Spot): void {
    const next = new Arrival(this.deps.socket, mapId, tickRateHz, at);
    this.stopListening();
    this.placed = false;
    this.veil.style.opacity = '1';
    const go = () => {
      this.fading = null;
      this.arriving = null; // the old map's teardown mustn't take the new one's messages
      this.teardown();
      this.build(next).catch(() => toasts.getState().push(`The map ${mapId} didn't load.`));
    };
    if (fade) {
      this.arriving = next; // a teardown during the fade lets go of it
      this.fading = setTimeout(go, FADE_MS);
      return;
    }
    go();
  }

  private stopListening(): void {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
  }

  /** Lets go of everyone drawn from the frames, as on a rejoin. */
  private clearOthers(): void {
    this.remotes?.clear();
    this.mobs.clear();
    this.mobAttacks = [];
    this.renderClock.reset();
    for (const [id, v] of this.remoteViews) {
      v.dispose();
      this.labels.delete(`p:${id}`);
    }
    this.remoteViews.clear();
    this.deadRemotes.clear();
    nearby.getState().clear();
    for (const v of this.mobViews.values()) {
      v.dispose();
    }
    this.mobViews.clear();
    for (const d of this.dying) {
      d.view.dispose();
    }
    this.dying.length = 0;
    this.drops.clear();
    for (const v of [...this.dropViews.values(), ...this.leaving]) {
      v.dispose();
    }
    this.dropViews.clear();
    this.leaving.length = 0;
  }

  /** Takes the map down: its scene and all it drew, its listeners and keys' state; the next build starts afresh. */
  private teardown(): void {
    this.visit++;
    if (this.fading !== null) {
      clearTimeout(this.fading);
      this.fading = null;
    }
    this.stopListening();
    this.arriving?.take(); // a build never finished, or a fade under way
    this.arriving = null;
    this.clearOthers();
    for (const { view } of this.npcViews.values()) {
      view.dispose();
    }
    this.npcViews.clear();
    this.me?.dispose();
    this.me = null;
    this.labels.clear();
    this.actions?.reset();
    this.actions = null;
    this.predictor = null;
    this.before = null;
    this.sender = null;
    this.potionKeys = null;
    this.skillKeys = null;
    this.attackHeld = false;
    this.myBeats = null;
    this.placed = false;
    this.transitionState = initialTransition;
    this.setStalled(false);
    this.camera = null;
    this.place = null;
    this.figures = null;
    this.lighting = null;
    this.map = null;
    this.scene?.dispose();
    this.scene = null;
  }
}
