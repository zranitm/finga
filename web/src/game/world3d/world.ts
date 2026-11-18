// The world in 3D (3d-world.md section 10): a map's scene as the server sends it, the player predicted over it, the keys sent, and a new scene at each map change.
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';

import { limits } from '@/gamedata/limits';
import { maps } from '@/gamedata/maps';
import { movement } from '@/gamedata/movement';
import { npcs as npcData } from '@/gamedata/npcs';
import { skillKeys } from '@/input/actions';
import type { Action } from '@/input/actions';
import { keyOf, keyTable } from '@/input/bindings';
import { isTyping } from '@/input/typing';
import { turn } from '@/lib/angle';
import { backoff } from '@/lib/backoff';
import { FixedStep, ticksIn, tickTiming } from '@/lib/fixed-step';
import type { TickTiming } from '@/lib/fixed-step';
import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import { actionPoints } from '@/progression/actions';
import { keyable } from '@/progression/chains';
import { mobById } from '@/progression/mobs';
import { MARKER_TEXT, markerFor, npcById, npcsOn } from '@/progression/quests';
import type { Npc } from '@/progression/quests';
import { bonusAt, moveSpeedWithout, skillById, usedStage } from '@/progression/skills';
import type { Skill, Stage as SkillStage } from '@/progression/skills';
import { moveSpeedWith } from '@/progression/stats';
import { blocked } from '@/progression/statuses';
import type { StatusOn } from '@/progression/statuses';
import { MobPose } from '@/protocol/messages';
import type { Data } from '@/protocol/messages';
import type { Input } from '@/sim/input';
import type { State, Way } from '@/sim/step';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { inventory } from '@/state/inventory';
import { keyboardLayout } from '@/state/keyboard-layout';
import { mapView } from '@/state/map-view';
import type { Spot as MapSpot } from '@/state/map-view';
import { nearby } from '@/state/nearby';
import { party } from '@/state/party';
import { pvp, walled } from '@/state/pvp';
import { questPlayer, quests } from '@/state/quests';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import type { ShownStatus } from '@/state/statuses';
import { statuses as shownStatuses } from '@/state/statuses';
import { target } from '@/state/target';
import type { Target } from '@/state/target';
import { teleport } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

import { loadArt, Fx } from '../effects/fx';
import type { Body } from '../effects/types';
import { warnOnce } from '../effects/warn';
import { groundImage } from '../map-image';
import { ActionClock, attackIn, attackTiming, skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';
import { aimYaw } from '../systems/aim';
import type { Mark } from '../systems/aim';
import { Arrival } from '../systems/arrival';
import type { Held, Spot } from '../systems/arrival';
import { ChainMemory, liveSkill } from '../systems/chains';
import type { ChainNow } from '../systems/chains';
import { InvulnFlash, parseRef } from '../systems/combat-view';
import type { Ref } from '../systems/combat-view';
import { departure, DropTracker } from '../systems/drops';
import type { DropChanges, Point } from '../systems/drops';
import { FeedWatch } from '../systems/feed-watch';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock } from '../systems/interpolation';
import type { RemoteLook } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { Lock } from '../systems/lock';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import { MobTracker } from '../systems/mobs';
import { PotionKeys } from '../systems/potion-keys';
import { Predictor, movementParams, wireState } from '../systems/prediction';
import type { MapKind, Mark as MoveMark, Pace } from '../systems/prediction';
import { healReach, SkillKeys } from '../systems/skill-keys';
import { moveStart, wayFor } from '../systems/skill-way';
import { relationOf } from '../systems/teams';

import { Bars } from './bars';
import { beatsOf } from './beats';
import type { Beats } from './beats';
import type { Figure, Gait, Lighting, Pose } from './body';
import { STILL, sunShadows } from './body';
import { Combat, playerBody } from './combat';
import { cutMobs } from './cut-mobs';
import { DropMark, warmGems } from './drop';
import { Figures } from './figures';
import { CHEST_M, cameraAt, easeShare, heldView, yawOf } from './follow';
import { Labels } from './labels';
import { LockRing } from './lock-ring';
import { classLook, mobLook, npcLook } from './looks';
import { groundUnder, loadMap, standsIn } from './map';
import type { WorldMap } from './map';
import type { ActionPose } from './model-looks';
import { Models } from './models';
import type { ModelSource } from './models';
import { NumberLayer } from './numbers';
import { Outline } from './outline';
import { Overlay } from './overlay';
import { buildPlace, fade } from './place';
import type { Place } from './place';
import { PrepWall } from './prep-wall';
import { ShieldBubbles } from './shield-bubbles';
import { teleportGlow } from './teleport-glow';
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
  /** The page's clock in ms, which the keys, actions and frames share. */
  readonly now: () => number;
}

type Wire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];
type DropWire = Data<'snapshot'>['drops'][number];
/** A living mob's mark, rewritten in place each frame. */
type LiveMark = { -readonly [K in keyof Mark]: Mark[K] };

const FADE_MS = 250;
const RETRY = { baseMs: 500, capMs: 10_000 }; // a map that didn't load tries again after a backoff
const BANNER_MS = 1800;
const STALL_MS = 2000; // 40 ticks without a frame: the world has stalled, though the socket is open
const SUN = new Vector3(0.35, -1, 0.6);
const SHADOW_MAP = 2048;
const TURN_MS = 60; // a turn eases most of the way in this
const MAX_FRAME_MS = 100; // a frame after a stall moves no further than this
const GLOW_SLACK_MS = 500; // the channel's glow lasts this past its length, for the MapChange to come
const MOVES: ReadonlySet<Action> = new Set(['left', 'right', 'up', 'down', 'jump']);
const MARKS_MS = 100; // how often the map views' marks move
const LOCK_INSET = 0.08; // the share of the canvas's width and height in from each edge a mob must be for the lock to pick it
const CORRECTION_MS = 100; // a reset's jump is drawn as a shove over this
const RUN_SPEED = 3; // m/s: from here a body runs rather than walks
const STILL_SPEED = 0.1;
const DYING_MS = 1000; // a killed mob lies this long before it goes
const NAME_LIFT_M = 0.35; // a name over its head
const MARKER_LIFT_PX = 18; // a marker over its name
const PORTAL_LABEL_M = 2.2;
const PROMPT_LIFT_PX = 42; // the talk prompt over an NPC's name and marker

/** A figure drawn for one of the map's bodies, made in the background; its turn eases toward its facing. */
class Drawn {
  figure: Figure | null = null;
  yaw: number;
  private gone = false;
  private last: { x: number; y: number; z: number } | null = null;
  private posed: { p: Pose; at: number } = { p: 'up', at: 0 };

  constructor(made: Promise<Figure>, yaw: number) {
    this.yaw = yaw;
    made.then(
      (f) => {
        if (this.gone) {
          f.dispose();
          return;
        }
        this.figure = f;
        if (this.posed.p !== 'up') {
          f.pose(this.posed.p, this.posed.at); // taken while it loaded
        }
      },
      () => undefined, // nothing to draw; drawn() says why
    );
  }

  /** Draws it at (x, y, z) turned toward yaw, its gait by gait, or else by how far it moved since the last frame. */
  draw(x: number, y: number, z: number, yaw: number, now: number, dtMs: number, gait?: Gait): void {
    const last = this.last;
    const moved = last ? Math.hypot(x - last.x, z - last.z) : 0;
    if (last) {
      last.x = x;
      last.y = y;
      last.z = z;
    } else {
      this.last = { x, y, z };
    }
    this.yaw = turn(this.yaw, yaw, Math.min(1, dtMs / TURN_MS));
    const f = this.figure;
    if (!f) {
      return;
    }
    f.root.position.set(x, y, z);
    f.root.rotation.y = this.yaw;
    f.update(now, dtMs, gait ?? gaitAt(dtMs > 0 ? (moved * 1000) / dtMs : 0, true));
  }

  act(kind: ActionPose, beats: Beats): void {
    this.figure?.act(kind, beats);
  }

  /** Shows or hides it, as the player's blink while invulnerable does. */
  show(on: boolean): void {
    this.figure?.root.setEnabled(on);
  }

  /** Draws it faded, as a dodge shows, or whole; set each frame. */
  fade(on: boolean): void {
    this.figure?.fade(on);
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

/** The clip a skill plays: a block's guard, else the class's skill. */
const actionOf = (skill: Skill): ActionPose => (skill.effect === 'block' ? 'block' : 'skill');

/** A body moving at speed m/s: still, walking or running. */
const GAITS = {
  still: [
    { pace: 'still', onGround: false },
    { pace: 'still', onGround: true },
  ],
  walk: [
    { pace: 'walk', onGround: false },
    { pace: 'walk', onGround: true },
  ],
  run: [
    { pace: 'run', onGround: false },
    { pace: 'run', onGround: true },
  ],
} as const satisfies Record<Gait['pace'], readonly [Gait, Gait]>;

function gaitAt(speed: number, onGround: boolean): Gait {
  const pace = speed < STILL_SPEED ? 'still' : speed < RUN_SPEED ? 'walk' : 'run';
  return GAITS[pace][onGround ? 1 : 0];
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
  private combat: Combat | null = null;
  private outline: Outline | null = null;
  private ring: LockRing | null = null;
  private bubbles: ShieldBubbles | null = null;
  private prepWall: PrepWall | null = null;
  private walledUp = false; // a match's prep wall stands, and attacks and skills wait for the round
  private remoteLooks: ReadonlyMap<string, RemoteLook> = new Map(); // as drawn this frame
  private lockedAt: MapSpot | null = null; // the lock as drawn, for the map views
  private lockedId: string | null = null; // and its ref, which the place opens round
  private readonly statusTimes = new Map<string, ShownStatus & { ends: number }>(); // by unit ref and status, as the HUD times them
  private marksDue = 0;
  private readonly overlay: Overlay;
  private readonly labels: Labels;
  private readonly bars: Bars;
  private readonly numbers: NumberLayer;
  private readonly veil: HTMLDivElement;
  private visit = 0; // each build and teardown starts the next; a build that finishes for an old one is thrown away
  private resets0 = 0; // the resets of the maps before this one
  private arriving: Arrival | null = null;
  private fading: ReturnType<typeof setTimeout> | null = null;
  private retrying: ReturnType<typeof setTimeout> | null = null;
  private bannerEl: { readonly el: HTMLElement; timer: ReturnType<typeof setTimeout> } | null =
    null;
  private offs: (() => void)[] = [];
  private glow: (() => void) | null = null; // the teleport channel's
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
  private latestMe: Wire | null = null; // the newest frame of the local player, which places them after a respawn here
  private transitionState: TransitionState = initialTransition;

  private predictor: Predictor | null = null;
  private before: State | null = null; // the step before the predictor's last, the player drawn between them
  private correction: {
    readonly x: number;
    readonly y: number;
    readonly z: number;
    readonly from: number;
  } | null = null; // from the player as drawn to where a reset put them, eased away
  private readonly sender: InputSender; // the page's keys, so one held through a map change keeps walking
  private actions: ActionClock | null = null;
  private attackHeld = false; // as the keys are: held through a map change
  private readonly flash = new InvulnFlash();
  private myDodge: { readonly from: number; readonly until: number } | null = null; // the own skill's dodge, from its start as predicted
  private myAim: { readonly yaw: number; readonly until: number; readonly target: string } | null =
    null; // the yaw the player's action turns them to, till its strike ends, and the lock's ref as it began
  private myWay: Way | null = null; // the way the player's moving skill goes, as its begin worked it out for its use to send
  private heldNow: Input | null = null; // the keys of the tick under way, which a skill's way along the keys reads
  private readonly remoteAims = new Map<string, { readonly yaw: number; readonly until: number }>();
  private readonly marks: LiveMark[] = []; // the mobs and, in a match, the enemies drawn this frame, which the lock holds, kept from frame to frame
  private readonly lock = new Lock();
  private readonly chainMemory = new ChainMemory(); // the player's latest hits and dodge on this map, as the world keeps them for chains
  private ackSeq = 0; // the input the newest frame acked
  private potionKeys: PotionKeys | null = null;
  private skillKeys: SkillKeys | null = null;

  private me: Drawn | null = null;
  private meCls = '';
  private readonly follow = new Vector3();
  private readonly lookAt = new Vector3();
  private readonly head = new Vector3();
  private remotes: RemotePlayers | null = null;
  private readonly remoteViews = new Map<string, Drawn>();
  private readonly deadRemotes = new Set<string>();
  private mobs = new MobTracker(this.timing.tickMs);
  private readonly mobViews = new Map<number, Drawn>();
  private readonly mobTypes = new Map<number, string>();
  private readonly dying: {
    readonly id: number;
    readonly type: string | undefined;
    readonly view: Drawn;
    readonly until: number;
  }[] = [];
  private readonly npcViews = new Map<
    string,
    { readonly view: Drawn; readonly npc: Npc; readonly y: number }
  >();
  private readonly drops = new DropTracker();
  private readonly dropViews = new Map<number, DropMark>();
  private readonly leaving: DropMark[] = [];

  private constructor(private readonly deps: WorldDeps) {
    this.sender = new InputSender({
      send: (input, tick, view) => deps.socket.sendInput(input, tick, view),
    });
    this.overlay = new Overlay(deps.parent);
    this.labels = new Labels(this.overlay);
    this.bars = new Bars(this.overlay);
    this.numbers = new NumberLayer(this.overlay);
    this.veil = document.createElement('div');
    this.veil.style.cssText = `position:absolute;inset:0;background:#0a0c16;opacity:1;pointer-events:none;transition:opacity ${FADE_MS}ms`;
    deps.parent.appendChild(this.veil);
  }

  /** Enters the world on the map arriving holds, built under the veil once its map and models are in. */
  static open(deps: WorldDeps, arriving: Arrival): World {
    const w = new World(deps);
    w.bindKeys();
    w.load(arriving);
    return w;
  }

  /** How often the world was found elsewhere than the player was predicted, outside a new map or a rejoin. */
  get resets(): number {
    return this.resets0 + (this.predictor?.resets ?? 0);
  }

  /** A match's prep wall up through prep and the count into a round, as the match's state says: drawn, and walked against from the next step. */
  private raiseWall(predictor: Predictor): void {
    const wall = this.map?.prepWall;
    const up = !!wall && walled(pvp.getState().match?.phase);
    if (!wall || up === this.walledUp) {
      return;
    }
    this.walledUp = up;
    this.prepWall?.show(up);
    predictor.setGeometry(up ? wall.walled : this.map!.geometry);
  }

  /** One frame of dtMs: the player stepped and sent at the world's tick, every body drawn, the camera eased after the player, then rendered. */
  frame(dtMs: number): void {
    const scene = this.scene;
    const predictor = this.predictor;
    const sender = this.sender;
    if (!scene || !predictor) {
      return;
    }
    const dt = Math.min(dtMs, MAX_FRAME_MS);
    const now = this.deps.now();
    this.setStalled(this.feed.stalled(now));
    this.raiseWall(predictor);
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
          const input = sender.tickStart(view); // sends it, so the socket's sequence is its own
          this.heldNow = input;
          predictor.beginTick(this.deps.socket.sequence, input, view);
        }
        this.before = predictor.state;
        predictor.subStep();
      }
    }
    this.drawMe(now, dt);
    this.drawRemotes(now, dt);
    this.showShields(now);
    this.drawMobs(now, dt);
    this.showLock(now);
    this.outline?.ready();
    this.showStatuses(now);
    this.showChains();
    this.publishMarks(now);
    this.drawNpcs(now, dt);
    this.drawDrops(now);
    this.moveCamera(dt);
    this.combat?.frame(now);
    this.closeDialogOutOfReach();
    this.showPrompt();
    skillBar.getState().endBuffs(now);
    if (this.camera) {
      this.overlay.frame(scene, this.camera);
      this.bars.place();
      this.labels.place();
      this.numbers.place(now);
    }
    scene.render();
  }

  dispose(): void {
    this.teardown();
    this.keysOff?.();
    this.keysOff = null;
    this.labels.dispose();
    this.bars.dispose();
    this.overlay.dispose();
    this.veil.remove();
  }

  /** Builds the map arriving names under the veil, again after a backoff each time it fails, until it stands or a teardown moves on. */
  private load(arriving: Arrival): void {
    const visit = ++this.visit;
    this.arriving = arriving;
    let attempt = 0;
    const tryBuild = () => {
      this.retrying = null;
      this.build(arriving, visit).catch((err: unknown) => {
        if (visit !== this.visit) {
          return;
        }
        console.info(
          `finga: map ${arriving.mapId} didn't load: ${err instanceof Error ? err.message : String(err)}`,
        );
        if (attempt === 0) {
          toasts.getState().push('The map didn’t load. Trying again…');
        }
        this.retrying = setTimeout(tryBuild, backoff(attempt++, RETRY));
      });
    };
    tryBuild();
  }

  /** Builds the map arriving names, then applies what it held; a map named meanwhile builds instead, and a teardown throws it away. */
  private async build(arriving: Arrival, visit: number): Promise<void> {
    const id = arriving.mapId;
    const [map, art] = await Promise.all([loadMap(id), loadArt()]);
    if (visit !== this.visit) {
      return;
    }
    if (arriving.mapId !== id) {
      return this.build(arriving, visit); // a MapChange or Welcome named another while it loaded
    }
    const scene = new Scene(this.deps.engine);
    let place: Place;
    let lighting: Lighting;
    let figures: Figures;
    let fx: Fx;
    try {
      const sun = new DirectionalLight('sun', SUN.normalizeToNew(), scene);
      lighting = { palette: new Palette(scene), shadows: sunShadows(sun, SHADOW_MAP) };
      const models = new Models(scene, this.deps.models);
      figures = new Figures(scene, lighting, models);
      fx = new Fx(scene, map, art, this.deps.now, lighting.palette);
      place = await buildPlace(scene, map, lighting, models);
    } catch (err) {
      scene.dispose();
      throw err;
    }
    if (visit !== this.visit) {
      scene.dispose();
      return;
    }
    if (arriving.mapId !== id) {
      scene.dispose();
      return this.build(arriving, visit);
    }
    this.scene = scene;
    this.lighting = lighting;
    this.figures = figures;
    this.place = place;
    this.map = map;
    this.camera = new TargetCamera('camera', Vector3.Zero(), scene);
    this.outline = new Outline(scene);
    this.ring = new LockRing(scene, map, lighting.palette);
    this.bubbles = new ShieldBubbles(scene);
    this.prepWall = map.prepWall && new PrepWall(scene, map.prepWall.boxes, map.geometry.terrain);
    fx.warm(); // every look's shaders built before a fight needs them
    warmGems(scene, lighting.palette);
    mapView
      .getState()
      .showMap(map.id, [map.geometry.size_x, map.geometry.size_z], groundImage(map));
    this.start(arriving, fx);
  }

  /** Starts the built map: its clocks and trackers, the player hidden until their frame, the NPCs and portal labels, and the socket's frames. */
  private start(arriving: Arrival, fx: Fx): void {
    const map = this.map!;
    const socket = this.deps.socket;
    const { characterId } = this.deps.player;
    this.mapId = map.id;
    this.tickRateHz = arriving.tickRateHz;
    this.timing = tickTiming(this.tickRateHz, movement.dt);
    this.combat = this.newCombat(fx);
    this.clock = new FixedStep(this.timing);
    this.renderClock = new RenderClock(this.timing.tickMs);
    this.mobs = new MobTracker(this.timing.tickMs);
    this.feed = new FeedWatch(STALL_MS);
    this.remotes = new RemotePlayers(characterId, this.timing.tickMs);
    this.actions = new ActionClock(this.timing.tickMs, this.deps.now, () => this.afterimage());
    this.predictor = new Predictor(
      map.geometry,
      this.timing,
      movementParams(this.kind(), this.moveSpeed()),
      (view) => this.mobs.blockersAt(view),
    );
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
      teleport.subscribe((now, before) => {
        if (now.started !== before.started || now.to !== before.to) {
          this.channelGlow(fx, now.to);
        }
      }),
      // A potion drunk from a key or the inventory shows what it restored over the drinker only.
      combatLog.subscribe((now, before) => {
        const line = now.lines.at(-1);
        if (line?.kind === 'drank' && line !== before.lines.at(-1)) {
          this.combat?.restored(line.hp);
        }
      }),
      socket.on('snapshot', (m) => this.onWorld(m)),
      socket.on('delta', (m) => this.onWorld(m)),
      socket.on('died', (m) => this.onWorld(m)),
      socket.on('respawned', (m) => this.onWorld(m)),
      socket.on('welcome', (m) => this.onWelcome(m.d.mapId, m.d.tickRateHz)),
      socket.on('mapChange', (m) => this.onTransition(m)),
      socket.on('damage', (m) => {
        this.combat?.damage(m.d, m.tick);
        this.guardedBy(m.d);
      }),
      socket.on('levelUp', () => this.combat?.levelUp()),
      socket.on('attackUsed', (m) => this.onAttackUsed(m.d)),
      socket.on('skillUsed', (m) => this.onSkillUsed(m.d)),
      socket.on('mobAttacked', (m) => this.combat?.mobAttacked(m.d, m.tick)),
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
          if (MOVES.has(a)) {
            teleport.getState().end(); // the next input carries the key, which the server cancels the channel for
          }
          if (a === 'attack') {
            this.attackHeld = true;
            if (!again) {
              this.attack(true); // a key held again after a release-all repeats as a held key does
            }
            return;
          }
          this.sender.press(a);
        },
        release: (a) => {
          if (a === 'attack') {
            this.attackHeld = false;
            return;
          }
          this.sender.release(a);
        },
        releaseAll: () => {
          this.attackHeld = false;
          this.sender.releaseAll();
        },
      },
      keys: () => keyTable(settings.getState().keys),
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
      case 'map':
      case 'arena':
        ui.getState().toggleWindow(action);
        return;
      case 'talk':
        this.talk();
        return;
      case 'nextTarget':
        this.nextTarget();
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
      this.latestMe = me;
      this.placeAt(me);
      this.flash.set(me.invulnerable, now);
    }
  }

  private onDelta(d: Data<'delta'>, tick: number): void {
    const now = this.deps.now();
    this.ackSeq = d.ackSeq;
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
    const acting = !this.restrained().act;
    this.latestMe = me;
    if (acting && this.restrained().act) {
      this.stopAction(now);
    }
    this.flash.set(me.invulnerable, now);
    if (!this.placed) {
      this.placeAt(me); // after a respawn here whose frame hadn't come
      return;
    }
    if (!this.transitionState.dead) {
      const resets = this.predictor.resets;
      this.predictor.reconcile(wireState(me), d.ackSeq, tick);
      if (this.predictor.resets !== resets) {
        this.corrected(now);
      }
    }
    character.getState().setHp(me.hp);
    character.getState().setShield(me.shield);
  }

  /** The world put the player elsewhere than predicted, as a knockback does: the sim takes it at once, and the drawn player eases there from where they were drawn. */
  private corrected(now: number): void {
    const p = this.predictor;
    const at = this.me?.at;
    if (!p || !at) {
      return;
    }
    const s = p.state;
    this.correction = { x: at.x - s.x, y: at.y - s.y, z: at.z - s.z, from: now }; // from where it's drawn, a correction still easing too
    this.before = s; // drawn from the new state on, not blended across the jump
  }

  /** How much of a correction is still drawn at now: all of it as it starts, easing out to none by CORRECTION_MS. */
  private easing(c: { readonly from: number }, now: number): number {
    const k = Math.min(1, Math.max(0, (now - c.from) / CORRECTION_MS));
    return (1 - k) * (1 - k);
  }

  /** Sets the prediction on the server's state of the player, and shows them. */
  private placeAt(me: Wire): void {
    if (!this.predictor) {
      return;
    }
    this.predictor.reset(wireState(me));
    this.before = this.predictor.state;
    this.correction = null; // a placement jumps
    if (!this.placed) {
      this.follow.set(me.x, me.y + CHEST_M, me.z);
      if (this.me) {
        this.me.yaw = yawOf(me.hx, me.hz);
      }
    }
    this.placed = true;
    character.getState().setHp(me.hp);
    character.getState().setShield(me.shield);
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
    const c = this.correction;
    const left = c ? this.easing(c, now) : 0;
    if (c && left === 0) {
      this.correction = null;
    }
    const x = a.x + (b.x - a.x) * f + (c?.x ?? 0) * left;
    const y = a.y + (b.y - a.y) * f + (c?.y ?? 0) * left;
    const z = a.z + (b.z - a.z) * f + (c?.z ?? 0) * left;
    const aim = this.myAim && now < this.myAim.until ? this.myAim.yaw : yawOf(b.hx, b.hz);
    me.draw(x, y, z, aim, now, dtMs, gaitAt(speed, b.on_ground));
    me.pose(this.transitionState.dead ? 'dead' : 'up', now);
    me.show(this.flash.shown(now));
    const dodge = this.myDodge;
    me.fade(dodge !== null && dodge.from <= now && now < dodge.until);
    if (me.figure) {
      this.combat?.ready(me.figure);
    }
    this.labels.set('me', 'self', this.deps.player.name, x, y + movement.height + NAME_LIFT_M, z);
  }

  private drawRemotes(now: number, dtMs: number): void {
    const remotes = this.remotes;
    const renderTime = this.renderClock.renderTime(now);
    if (!remotes || renderTime === null) {
      return;
    }
    const { shown, looks, gone } = remotes.render(renderTime);
    this.remoteLooks = looks;
    let changed = gone.length > 0;
    for (const id of gone) {
      this.remoteViews.get(id)?.dispose();
      this.remoteViews.delete(id);
      this.labels.delete(`p:${id}`);
      this.remoteAims.delete(id);
      this.combat?.forget(id);
    }
    for (const [id, s] of shown) {
      let v = this.remoteViews.get(id);
      if (!v) {
        v = this.drawn(classLook(remotes.classOf(id)), yawOf(s.hx, s.hz));
        this.remoteViews.set(id, v);
        changed = true;
      }
      const aim = this.remoteAims.get(id);
      v.draw(
        s.x,
        s.y,
        s.z,
        aim && now < aim.until ? aim.yaw : yawOf(s.hx, s.hz),
        now,
        dtMs,
        gaitAt(Math.hypot(s.vx, s.vz), s.on_ground),
      );
      v.pose(this.deadRemotes.has(id) ? 'dead' : 'up', now);
      v.fade(looks.get(id)?.dodging ?? false);
      const info = remotes.infoOf(id);
      const side = relationOf(this.latestMe?.team ?? 0, info?.team ?? 0);
      this.labels.set(
        `p:${id}`,
        side === 'none' ? 'player' : side,
        remotes.nameOf(id),
        s.x,
        s.y + movement.height + NAME_LIFT_M,
        s.z,
        0,
        info?.bot ? 'BOT' : '',
      );
    }
    if (changed) {
      nearby
        .getState()
        .setNearby([...this.remoteViews.keys()].map((id) => ({ id, name: remotes.nameOf(id) })));
    }
  }

  /** The server's time the drawn player stands at, which a mob's strike at them is drawn on; the render time before an ack, or once it passes a player held still by death or a stall. */
  private playerTime(renderTime: number): number {
    const tick = this.predictor?.tickAt(this.clock.fraction);
    return Math.max(renderTime, tick == null ? renderTime : tick * this.timing.tickMs);
  }

  private drawMobs(now: number, dtMs: number): void {
    const renderTime = this.renderClock.renderTime(now);
    if (renderTime === null) {
      return;
    }
    const { shown, gone } = this.mobs.render(renderTime);
    const carried = this.combat?.mobsDrawn(renderTime, this.playerTime(renderTime), now, shown);
    for (const id of gone) {
      const v = this.mobViews.get(id);
      const type = this.mobTypes.get(id);
      this.mobViews.delete(id);
      this.mobTypes.delete(id);
      this.bars.delete(id);
      if (v) {
        v.pose('dead', now);
        this.dying.push({ id, type, view: v, until: now + DYING_MS });
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
    let marked = 0;
    for (const [id, wire] of shown) {
      const m = mobById(wire.type);
      const c = carried?.get(id);
      const s = c ? { ...wire, ...c } : wire; // drawn, and aimed at, where its hop or charge carries it
      const mark = this.markAt(marked++);
      mark.id = `m:${id}`;
      mark.x = s.x;
      mark.z = s.z;
      mark.radius = m?.radius ?? 0;
      mark.hp = s.hp;
      const middle = s.y + (m?.height ?? 0) / 2; // seen through last frame's camera
      mark.inView = this.overlay.within(s.x, middle, s.z, LOCK_INSET);
      mark.inFrame = this.overlay.within(s.x, middle, s.z, 0);
      if (m && !m.boss && s.hp > 0) {
        this.bars.set(id, s.x, s.y + m.height, s.z, s.hp, s.maxHp, m.elite);
      } else {
        this.bars.delete(id); // a boss's is the bar at the top; a killed one's is gone
      }
      let v = this.mobViews.get(id);
      if (!v) {
        v = this.drawn(mobLook(s.type), s.yaw);
        this.mobViews.set(id, v);
        this.mobTypes.set(id, s.type);
      }
      v.draw(s.x, s.y, s.z, s.yaw, now, dtMs);
      v.pose(poseOf(s.pose), now - s.poseMs);
    }
    this.marks.length = this.markFoes(marked);
    const boss = fieldBoss.getState().boss;
    const drawn = boss && shown.get(boss.mobId);
    fieldBoss.getState().setHp(drawn ? { hp: drawn.hp, maxHp: drawn.maxHp } : null); // the boss bar shows what is drawn
  }

  /** The mark kept at i, made the first time. */
  private markAt(i: number): LiveMark {
    return (this.marks[i] ??= {
      id: '',
      x: 0,
      z: 0,
      radius: 0,
      hp: 0,
      inView: false,
      inFrame: false,
    });
  }

  /** In a match, the enemies drawn this frame marked after the mobs, from i on, for the lock to hold too: the count of marks after them. */
  private markFoes(i: number): number {
    const remotes = this.remotes;
    const mine = this.latestMe?.team ?? 0;
    if (!remotes || mine === 0) {
      return i;
    }
    let marked = i;
    for (const [id, v] of this.remoteViews) {
      const p = remotes.infoOf(id);
      const at = v.at;
      if (!p || !at || relationOf(mine, p.team) !== 'foe') {
        continue;
      }
      const mark = this.markAt(marked++);
      mark.id = `p:${id}`;
      mark.x = at.x;
      mark.z = at.z;
      mark.radius = movement.radius;
      mark.hp = this.deadRemotes.has(id) ? 0 : p.hp;
      const middle = at.y + movement.height / 2;
      mark.inView = this.overlay.within(at.x, middle, at.z, LOCK_INSET);
      mark.inFrame = this.overlay.within(at.x, middle, at.z, 0);
    }
    return marked;
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

  /** Eases the camera toward the player's chest by the time passed, held so the map's edge stops it, fades what stands between them, and opens the place round the chest and the mobs fighting them. */
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
    const look = heldView(
      this.follow,
      map.geometry.size_x,
      map.geometry.size_z,
      aspect,
      this.lookAt,
    );
    cameraAt(look.x, look.y, look.z, camera.position);
    camera.setTarget(look);
    if (this.place) {
      fade(
        camera.position,
        this.head.set(at.x, at.y + movement.height, at.z),
        this.place.looks,
        dtMs,
      );
    }
    const opened = cutMobs(
      at.x,
      at.z,
      this.lockedId,
      (ref) => {
        const r = parseRef(ref);
        return r?.kind === 'mob' && (this.combat?.attacking(r.id) ?? false);
      },
      this.marks,
    );
    this.lighting?.palette.cutAround(
      at.x,
      at.y + CHEST_M,
      at.z,
      opened.flatMap((m) => {
        const r = parseRef(m.id);
        const body = r && this.bodyOf(r);
        return body ? [body] : [];
      }),
    );
  }

  /** The unit a ref names as drawn: a mob, the player or another player. */
  private drawnOf(r: Ref): Drawn | null | undefined {
    if (r.kind === 'mob') {
      return this.mobViews.get(r.id);
    }
    return r.id === this.deps.player.characterId ? this.me : this.remoteViews.get(r.id);
  }

  /** The fight drawn on this map, reading who's drawn where from the views. */
  private newCombat(fx: Fx): Combat {
    const me = this.deps.player.characterId;
    return new Combat(
      {
        me,
        now: this.deps.now,
        tickMs: this.timing.tickMs,
        body: (r) => this.bodyOf(r),
        meAtTickEnd: () => {
          const p = this.predictor?.tickEnd();
          return this.placed && p ? playerBody(p.x, p.y, p.z) : undefined;
        },
        figure: (r) => this.drawnOf(r)?.figure ?? null,
        mobType: (id) => this.mobTypes.get(id),
        mobAt: (id, tick) => this.mobs.placeAt(id, tick),
        mobPoses: (id) => this.mobs.posesOf(id),
        mobGone: (id) => this.mobs.goneAt(id),
        healedBy: (stage) => this.healedBy(stage),
      },
      fx,
      this.numbers,
    );
  }

  /** Where a player or mob is drawn now: the player as predicted, once placed, and a killed mob as it lies; undefined for one not drawn here. */
  /** A bubble round each player drawn with shield left: the player's own as the newest frame has it, others' as drawn. */
  private showShields(now: number): void {
    const on = new Map<string, { x: number; y: number; z: number }>();
    const mine = this.me?.figure;
    if (mine && (this.latestMe?.shield ?? 0) > 0) {
      on.set('me', mine.root.position);
    }
    for (const [id, v] of this.remoteViews) {
      if (v.figure && (this.remoteLooks.get(id)?.shield ?? 0) > 0) {
        on.set(`p:${id}`, v.figure.root.position);
      }
    }
    this.bubbles?.show(on, now);
  }

  private bodyOf(r: Ref): Body | undefined {
    if (r.kind === 'mob') {
      const dead = this.mobViews.has(r.id) ? undefined : this.dying.find((d) => d.id === r.id);
      const at = (dead?.view ?? this.mobViews.get(r.id))?.at;
      const m = mobById(dead ? dead.type : this.mobTypes.get(r.id));
      return at && m && { ...at, radius: m.radius, height: m.height };
    }
    if (r.id === this.deps.player.characterId) {
      const p = this.predictor?.state;
      return this.placed && p ? playerBody(p.x, p.y, p.z) : undefined;
    }
    const at = this.remoteViews.get(r.id)?.at;
    return at && playerBody(at.x, at.y, at.z);
  }

  /** A figure for look, made in the background; a model that fails to load, but for its map going first, says why once. */
  private drawn(look: Parameters<Figures['make']>[0], yaw: number): Drawn {
    const { figures, scene } = this;
    const made = figures ? figures.make(look) : Promise.reject(new Error('no scene'));
    made.catch((err: unknown) => {
      if (scene && !scene.isDisposed) {
        warnOnce(
          `${look.file} didn't load (${err instanceof Error ? err.message : String(err)}), so it isn't drawn`,
        );
      }
    });
    return new Drawn(made, yaw);
  }

  /** The player's class: the store's, which a promotion changes, else the one the world was entered with. */
  private get cls(): string {
    return character.getState().character?.class ?? this.deps.player.cls;
  }

  private kind(): MapKind {
    return maps.find((m) => m.id === this.mapId)?.kind ?? 'town';
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

  /** The yaw an action aims along: to the lock, in or out of its reach, else the player's heading, as a heal, aiming at no one, always does; and the lock's ref for the world to hit first, "" for none. */
  private aimFor(heals: boolean): { readonly yaw: number; readonly target: string } {
    const s = this.predictor?.state;
    if (!s) {
      return { yaw: 0, target: '' };
    }
    const heading = yawOf(s.hx, s.hz);
    if (heals) {
      return { yaw: heading, target: '' };
    }
    const locked = this.lock.update(s.x, s.z, this.marks);
    return { yaw: aimYaw(s.x, s.z, heading, locked), target: locked?.id ?? '' };
  }

  /** Each unit drawn with its statuses as the newest frames list them, each until its end tick: their marks on them, and the HUD's icons for the player, the lock and party members here. */
  private showStatuses(now: number): void {
    const tick = this.lastTick;
    const units = new Map<string, readonly StatusOn[]>();
    const put = (ref: string, on: readonly StatusOn[]) => {
      units.set(
        ref,
        on.filter((s) => s.ends > tick),
      );
    };
    const me = this.latestMe;
    if (me && this.me) {
      put(`p:${me.id}`, me.statuses);
    }
    for (const id of this.remoteViews.keys()) {
      put(`p:${id}`, this.remotes?.statusesOf(id) ?? []);
    }
    for (const id of this.mobViews.keys()) {
      put(`m:${id}`, this.mobs.statusesOf(id));
    }
    this.combat?.statuses(units);
    this.publishStatuses(units, now);
  }

  /** The HUD's statuses: the player's own, the locked mob's, and each party member's on this map, each timed from when it first showed, or restarted, to its end tick. */
  private publishStatuses(units: ReadonlyMap<string, readonly StatusOn[]>, now: number): void {
    const seen = new Set<string>();
    const timed = (ref: string): ShownStatus[] =>
      (units.get(ref) ?? []).map((s) => {
        const key = `${ref} ${s.id}`;
        seen.add(key);
        let t = this.statusTimes.get(key);
        if (t?.ends !== s.ends) {
          t = {
            id: s.id,
            ends: s.ends,
            from: now,
            until: now + (s.ends - this.lastTick) * this.timing.tickMs,
          };
          this.statusTimes.set(key, t);
        }
        return { id: t.id, from: t.from, until: t.until };
      });
    const me = this.deps.player.characterId;
    const here: Record<string, ShownStatus[]> = {};
    for (const m of party.getState().party?.members ?? []) {
      if (m.characterId !== me && m.mapId === this.mapId) {
        here[m.characterId] = timed(`p:${m.characterId}`);
      }
    }
    shownStatuses.getState().show({
      me: timed(`p:${me}`),
      target: this.lockedId === null ? [] : timed(this.lockedId),
      party: here,
    });
    for (const key of this.statusTimes.keys()) {
      if (!seen.has(key)) {
        this.statusTimes.delete(key);
      }
    }
  }

  /** Keeps the lock on the units drawn this frame and marks it with the outline and the ring, on a map with combat while the player may act; meanwhile the lock waits. */
  private showLock(now: number): void {
    const s = this.predictor?.state;
    const locked =
      s && this.kind() !== 'town' && this.free() ? this.lock.update(s.x, s.z, this.marks) : null;
    const ref = locked ? parseRef(locked.id) : undefined;
    const view = ref && this.drawnOf(ref);
    const at = view?.at;
    this.outline?.mark(view?.figure ?? null);
    this.ring?.show(locked && at ? { ...at, radius: locked.radius } : null, now);
    this.lockedAt = locked && at ? { x: at.x, z: at.z } : null;
    this.lockedId = locked?.id ?? null;
    target.getState().show(locked && ref ? this.targetOf(ref, locked) : null);
  }

  /** The target window's lock: a mob's kind and its HP as drawn, a player's name, class, level and HP as of the newest frame. */
  private targetOf(ref: Ref, m: Mark): Target | null {
    if (ref.kind === 'mob') {
      const type = this.mobTypes.get(ref.id);
      return type ? { kind: 'mob', ref: m.id, type, hp: m.hp } : null;
    }
    const p = this.remotes?.infoOf(ref.id);
    return p
      ? {
          kind: 'player',
          ref: m.id,
          name: p.name,
          cls: p.cls,
          level: p.level,
          hp: m.hp,
          maxHp: p.maxHp,
          bot: p.bot,
        }
      : null;
  }

  /** A unit's statuses as of the newest frame, by its ref. */
  private statusesAt(ref: string): readonly StatusOn[] {
    const r = parseRef(ref);
    if (r?.kind === 'mob') {
      return this.mobs.statusesOf(r.id);
    }
    return r ? (this.remotes?.statusesOf(r.id) ?? []) : [];
  }

  /** What each key offers now, for the skill bar: its root, or the follow-up its chain gives now, judged on the lock as drawn. */
  private showChains(): void {
    const bar = skillBar.getState();
    const cls = character.getState().character?.class ?? '';
    const n = this.chainNow(this.marks.find((m) => m.id === this.lockedId) ?? null);
    bar.showLive(bar.slots.map((root) => (root && n ? liveSkill(cls, root, n) : root)));
  }

  /** What chains are judged by now, with the lock as given: the input a press would go with, the player's points and cooldowns, their latest hits and dodge, and how far the lock is and its statuses as of the newest frame. */
  private chainNow(locked: Mark | null): ChainNow | null {
    const c = character.getState().character;
    const s = this.predictor?.state;
    if (!c || !s) {
      return null;
    }
    return {
      seq: this.deps.socket.sequence,
      tickMs: this.timing.tickMs,
      now: this.deps.now(),
      invested: c.sheet?.skills?.invested ?? {},
      cooldowns: skillBar.getState().cooldowns,
      hits: this.chainMemory.hits,
      dodged: this.chainMemory.dodged,
      blocked: this.chainMemory.blocked,
      held: this.restrained().held,
      cantAct: this.restrained().act,
      lock: locked && {
        far: Math.hypot(locked.x - s.x, locked.z - s.z),
        statuses: this.statusesAt(locked.id),
        tick: this.lastTick,
      },
    };
  }

  /** A mob's attack passed through the player's own dodge, or their block stopped it: the world sends a tick's frame before its events, so the newest frame acked the input it counts the counter from. */
  private guardedBy(d: Data<'damage'>): void {
    const to = parseRef(d.target);
    if (to?.kind !== 'player' || to.id !== this.deps.player.characterId) {
      return;
    }
    if (d.dodge) {
      this.chainMemory.dodge(this.ackSeq);
    } else if (d.block) {
      this.chainMemory.block(this.ackSeq);
    }
  }

  /** Where the map views mark the player, the party members on this map, the lock and the field boss, as drawn, about ten times a second. */
  private publishMarks(now: number): void {
    if (now < this.marksDue) {
      return;
    }
    this.marksDue = now + MARKS_MS;
    const s = this.predictor?.state;
    const me = this.me?.at;
    const members = party.getState().party?.members ?? [];
    const boss = fieldBoss.getState().boss;
    const bossAt = boss ? this.mobViews.get(boss.mobId)?.at : undefined;
    mapView.getState().mark({
      me: me && s ? { x: me.x, z: me.z, yaw: yawOf(s.hx, s.hz) } : null,
      party: members.flatMap((m) => {
        const at = this.remoteViews.get(m.characterId)?.at; // drawn here, so on this map
        return at ? [{ id: m.characterId, name: m.name, x: at.x, z: at.z }] : [];
      }),
      lock: this.lockedAt,
      boss: bossAt ? { x: bossAt.x, z: bossAt.z } : null,
    });
  }

  /** Tab: locks the next mob or enemy farther than the locked one, on a map with combat while the player may act. */
  private nextTarget(): void {
    const s = this.predictor?.state;
    if (s && this.kind() !== 'town' && this.free()) {
      this.lock.next(s.x, s.z, this.marks);
    }
  }

  /** A fresh press or a held key attacks as soon as an attack may start: at a cancelable skill's cancel point, or as the running action ends; a held key never goes ahead of a press waiting for its moment. */
  private attack(fresh: boolean): void {
    const clock = this.actions;
    if (
      this.kind() === 'town' ||
      this.walledUp ||
      !this.free() ||
      !clock ||
      this.restrained().act
    ) {
      return; // the server runs no combat on town maps or before a round, and drops an attack a status stops
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
    const { yaw: aim, target } = this.aimFor(false);
    if (!this.deps.socket.sendAction('attack', { aim, target })) {
      return;
    }
    teleport.getState().end(); // an attack starting cancels the channel
    const timing = attackTiming(
      character.getState().character?.sheet,
      this.cls,
      this.timing.tickMs,
    );
    clock.begin(timing);
    const beats = this.act(timing, 'attack');
    this.myAim = { yaw: aim, until: beats.strikeEnd, target }; // facing free once it has struck, as a skill's
    const p = this.predictor?.state;
    if (p) {
      this.combat?.attack(
        this.deps.player.characterId,
        this.cls,
        beats,
        playerBody(p.x, p.y, p.z),
        aim,
      );
    }
  }

  /** A status that stops acting ends the player's own action, as the world ends it: its swing and look go, a flinch takes over, and no press waits for its moment. */
  private stopAction(now: number): void {
    this.actions?.reset();
    this.myAim = null;
    this.myDodge = null;
    this.me?.figure?.pose('flinch', now);
    this.combat?.stopped(this.deps.player.characterId);
  }

  /** What the player's statuses keep them from, as the world last sent them. */
  private restrained(): { act: boolean; skills: boolean; held: boolean } {
    return blocked(this.latestMe?.statuses ?? [], this.lastTick);
  }

  /** The player's own action drawn from now over its beats. */
  private act(timing: ActionTiming, kind: ActionPose): Beats {
    const beats = beatsOf(timing, this.deps.now(), this.timing.tickMs);
    this.me?.act(kind, beats);
    return beats;
  }

  /** Another player's basic attack, drawn over its length; the player's own, already drawn, says the world counted it. */
  private onAttackUsed(d: Data<'attackUsed'>): void {
    if (d.attacker === this.deps.player.characterId) {
      this.actions?.counted();
      return;
    }
    const v = this.remoteViews.get(d.attacker);
    const cls = this.remotes?.classOf(d.attacker);
    const at = v?.at;
    if (!v || !cls || !at) {
      return;
    }
    const beats = beatsOf(attackIn(cls, d.lengthTicks), this.deps.now(), this.timing.tickMs);
    v.act('attack', beats);
    this.remoteAims.set(d.attacker, { yaw: d.aim, until: beats.strikeEnd });
    this.combat?.attack(d.attacker, cls, beats, playerBody(at.x, at.y, at.z), d.aim);
  }

  /** Another player's skill, drawn from its arrival at the stage its trait gives; the player's own buff is timed here for the character window. */
  private onSkillUsed(d: Data<'skillUsed'>): void {
    const skill = skillById(d.skill);
    const stage = skill && usedStage(skill, d.stage, d.trait);
    if (!skill || !stage) {
      return;
    }
    if (d.caster !== this.deps.player.characterId) {
      const v = this.remoteViews.get(d.caster);
      const at = v?.at;
      if (v && at) {
        const timing = skillTiming(stage, this.timing.tickMs);
        const beats = beatsOf(timing, this.deps.now(), this.timing.tickMs);
        v.act(actionOf(skill), beats);
        this.remoteAims.set(d.caster, { yaw: d.aim, until: beats.strikeEnd });
        this.combat?.skill(
          d.caster,
          skill,
          stage,
          beats,
          playerBody(at.x, at.y, at.z),
          d.aim,
          parseRef(d.target),
        );
      }
      return;
    }
    if (skill.effect === 'buff') {
      const points = character.getState().character?.sheet?.skills?.invested[d.skill] ?? 0;
      skillBar.getState().buff({
        skill: d.skill,
        points,
        trait: d.trait,
        until: this.deps.now() + stage.duration_ms,
      });
    }
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

  /** A moving skill's way, kept for its use to send, and its move marked to start from the input after the press, as the world starts it, a break-free ending the hold first; null for a skill that neither moves nor breaks free. */
  private startMove(skill: Skill, stage: SkillStage, target: string): MoveMark | null {
    const s = this.predictor?.state;
    const move = stage.move;
    const lock = this.marks.find((m) => m.id === target) ?? null;
    this.myWay = move && s ? wayFor(move, s, this.heldNow, lock) : null;
    if (!this.predictor) {
      return null;
    }
    const seq = this.deps.socket.sequence;
    if (!move || !this.myWay) {
      return skill.breaks_control ? this.predictor.mark(seq, (at) => at, true) : null;
    }
    const steps = ticksIn(move.time_ms, this.timing.tickMs) * this.timing.stepsPerTick;
    const after = seq + (move.delay_ms > 0 ? ticksIn(move.delay_ms, this.timing.tickMs) : 0); // ticksIn holds any time to a tick at least
    return this.predictor.mark(after, moveStart(move, this.myWay, steps), skill.breaks_control);
  }

  /** The player's own skill starts as sent: its action, beats, pace and move; the undo, for a refusal, takes them back. */
  private beginSkill(skill: Skill, stage: SkillStage): () => void {
    teleport.getState().end(); // a skill starting cancels the channel
    const timing = skillTiming(stage, this.timing.tickMs);
    const giveBack = this.actions?.begin(timing) ?? (() => undefined);
    const paces = this.buffPaces(skill, stage, timing);
    const beats = this.act(timing, actionOf(skill));
    const aim = { ...this.aimFor(skill.effect === 'heal'), until: beats.strikeEnd };
    this.myAim = aim;
    const dodge =
      stage.dodge_ms > 0 ? { from: beats.start, until: beats.start + stage.dodge_ms } : null;
    if (dodge) {
      this.myDodge = dodge;
    }
    const mark = this.startMove(skill, stage, aim.target);
    const p = this.predictor?.state;
    if (p) {
      this.combat?.skill(
        this.deps.player.characterId,
        skill,
        stage,
        beats,
        playerBody(p.x, p.y, p.z),
        aim.yaw,
        parseRef(aim.target),
      );
    }
    return () => {
      giveBack();
      if (this.myAim === aim) {
        this.myAim = null;
      }
      if (dodge && this.myDodge === dodge) {
        this.myDodge = null;
      }
      if (mark) {
        this.predictor?.dropMark(mark);
      }
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
              traits: c.sheet?.skills?.traits ?? {},
              energy: energy.getState().shown,
              cooldownSpeed: c.sheet?.derived?.cooldownSpeed ?? 0,
              dead: this.transitionState.dead,
              cantAct: this.restrained().act,
              silenced: this.restrained().skills,
              held: this.restrained().held,
              hurt: c.hp < c.maxHp,
              feet: this.placed && p ? { x: p.x, y: p.y, z: p.z } : null,
              hurtAllies: this.hurtAllies(c.id).map((a) => a.at),
            }
          : null;
      },
      inTown: () => this.kind() === 'town',
      notStarted: () => this.walledUp,
      tickMs: this.timing.tickMs,
      now: this.deps.now,
      cooldowns: () => skillBar.getState().cooldowns,
      cool: (skill, cooldown) => skillBar.getState().cool(skill, cooldown),
      keyable: (skill) => keyable(character.getState().character?.class ?? '', skill),
      live: (root) => {
        const s = this.predictor?.state;
        const locked = s ? this.lock.update(s.x, s.z, this.marks) : null; // the lock the press would aim at
        const n = this.chainNow(locked);
        return n ? liveSkill(character.getState().character?.class ?? '', root, n) : root;
      },
      remember: (skill, stage) =>
        this.chainMemory.used(
          skill,
          socket.sequence,
          actionPoints(skillTiming(stage, this.timing.tickMs).ticks, stage.hit, stage.cancel).hit,
        ),
      use: (skill, root) =>
        socket.request(
          'useSkill',
          {
            skill,
            chainRoot: root,
            aim: this.myAim?.yaw ?? 0,
            target: this.myAim?.target ?? '',
            ...(this.myWay && { way: this.myWay }),
          },
          socket.sequence,
        ), // the aim, lock and way its begin just took
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

  /** The teleport channel's glow round the player, from the Response till the channel ends; a moment past its length, should no MapChange or cancel come. */
  private channelGlow(fx: Fx, to: string | null): void {
    this.glow?.();
    this.glow =
      to === null
        ? null
        : teleportGlow(
            fx,
            fx.now() + limits.teleport_channel_ms + GLOW_SLACK_MS,
            () => this.me?.at,
          );
  }

  /** The player's own cancel: the pose it cut off, left where it's drawn. */
  private afterimage(): void {
    const me = this.me;
    const at = me?.at;
    if (me && at) {
      this.combat?.afterimage(at, me.yaw, me.figure);
    }
  }

  /** Whom the player's own heal finds, as the world picks: themselves if hurt, then the nearest hurt members it reaches. */
  private healedBy(stage: SkillStage): string[] {
    const c = character.getState().character;
    const feet = this.predictor?.state;
    if (!c || !feet) {
      return [];
    }
    const radius = stage.shape?.radius ?? 0;
    const allies = this.hurtAllies(c.id)
      .flatMap((a) => (a.at ? [{ ref: `p:${a.id}`, d: healReach(feet, a.at) }] : []))
      .filter((a) => a.d <= radius)
      .sort((a, b) => a.d - b.d)
      .map((a) => a.ref);
    return [...(c.hp < c.maxHp ? [`p:${c.id}`] : []), ...allies].slice(0, stage.targets);
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

  /** The talk key's prompt over the portal the player stands in, else over the NPC in reach whose dialog isn't open (key-bindings.md section 6). */
  private showPrompt(): void {
    const p = this.predictor?.state;
    const map = this.map;
    const key = keyOf(
      keyTable(settings.getState().keys),
      'talk',
      keyboardLayout.getState().printed,
    );
    const portal = p && map && this.free() ? standsIn(p, map) : undefined;
    const npc =
      p && !portal && this.free() && !ui.getState().windows.includes('npc')
        ? this.nearestInReach(p)
        : undefined;
    if (portal && map) {
      const y = groundUnder(map, portal.x, portal.z) + PORTAL_LABEL_M;
      this.labels.set('prompt', 'prompt', `${key} Enter`, portal.x, y, portal.z, MARKER_LIFT_PX);
    } else if (npc) {
      const y = (this.npcViews.get(npc.id)?.y ?? 0) + movement.height + NAME_LIFT_M;
      this.labels.set('prompt', 'prompt', `${key} Talk`, npc.x, y, npc.z, PROMPT_LIFT_PX);
    } else {
      this.labels.set('prompt', 'prompt', '', 0, 0, 0);
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
    const banner = {
      el,
      timer: setTimeout(() => {
        el.style.opacity = '0';
        banner.timer = setTimeout(() => this.dropBanner(), 2 * FADE_MS);
      }, BANNER_MS),
    };
    this.dropBanner();
    this.bannerEl = banner;
  }

  private dropBanner(): void {
    if (this.bannerEl) {
      clearTimeout(this.bannerEl.timer);
      this.bannerEl.el.remove();
      this.bannerEl = null;
    }
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
    this.lock.clear();
    this.chainMemory.clear(); // a rejoin is a new player to the world
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
        this.latestMe = null; // a frame from before the death can't place a respawn
        this.flash.set(false, this.deps.now());
        this.myDodge = null;
        this.actions?.reset();
        this.lock.clear();
        skillBar.getState().endBuffs(); // the world ends them at death
        character.getState().setDead(true);
        ui.getState().closeWindow('npc');
        return;
      case 'respawn-here':
        this.lock.clear();
        character.getState().setHp(a.hp);
        character.getState().setDead(false);
        if (this.latestMe && !this.latestMe.dead) {
          this.placeAt(this.latestMe); // the world sends a tick's frame before its events, so the respawn's frame came first
        } else {
          this.placed = false; // its frame is still to come
        }
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
      this.load(next);
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
    this.combat?.clear();
    this.renderClock.reset();
    for (const [id, v] of this.remoteViews) {
      v.dispose();
      this.labels.delete(`p:${id}`);
    }
    this.remoteViews.clear();
    this.remoteAims.clear();
    this.deadRemotes.clear();
    nearby.getState().clear();
    for (const v of this.mobViews.values()) {
      v.dispose();
    }
    this.mobViews.clear();
    this.mobTypes.clear();
    this.bars.clear();
    this.marks.length = 0;
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
    this.resets0 += this.predictor?.resets ?? 0;
    if (this.fading !== null) {
      clearTimeout(this.fading);
      this.fading = null;
    }
    if (this.retrying !== null) {
      clearTimeout(this.retrying);
      this.retrying = null;
    }
    this.dropBanner();
    this.latestMe = null;
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
    this.lock.clear();
    this.chainMemory.clear();
    this.predictor = null;
    this.before = null;
    this.correction = null;
    this.potionKeys = null;
    this.skillKeys = null;
    this.combat?.dispose();
    this.combat = null;
    this.outline?.dispose();
    this.outline = null;
    this.ring?.dispose();
    this.ring = null;
    this.bubbles?.dispose();
    this.bubbles = null;
    this.prepWall?.dispose();
    this.prepWall = null;
    this.walledUp = false;
    this.remoteLooks = new Map();
    target.getState().show(null);
    mapView.getState().clear();
    this.lockedAt = null;
    this.lockedId = null;
    this.marksDue = 0;
    this.myAim = null;
    this.myDodge = null;
    this.overlay.reset();
    this.flash.set(false, 0);
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
