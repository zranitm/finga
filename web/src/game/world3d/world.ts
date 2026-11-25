// The world in 3D (3d-world.md section 10): a map's scene as the server sends it, the player predicted over it, the keys sent, and a new scene at each map change.
import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import type { AbstractEngine } from '@babylonjs/core/Engines/abstractEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { PrecisionDate } from '@babylonjs/core/Misc/precisionDate';
import { Scene } from '@babylonjs/core/scene';

import { actions as actionData } from '@/gamedata/actions';
import { interest } from '@/gamedata/interest';
import { limits } from '@/gamedata/limits';
import { loot as lootData } from '@/gamedata/loot';
import { maps } from '@/gamedata/maps';
import { movement } from '@/gamedata/movement';
import { npcs as npcData } from '@/gamedata/npcs';
import { skills } from '@/gamedata/skills';
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
import type { Data, PartyView } from '@/protocol/messages';
import type { Input } from '@/sim/input';
import { endSprint, sprintSteps } from '@/sim/step';
import type { State, Way } from '@/sim/step';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { energy } from '@/state/energy';
import { fieldBoss } from '@/state/field-boss';
import { fighters } from '@/state/fighters';
import type { FighterLive } from '@/state/fighters';
import { inventory } from '@/state/inventory';
import { keyboardLayout } from '@/state/keyboard-layout';
import { mapView } from '@/state/map-view';
import type { Spot as MapSpot } from '@/state/map-view';
import { nearby } from '@/state/nearby';
import { isMember, party } from '@/state/party';
import type { PartyState } from '@/state/party';
import { picksInReach, pouches } from '@/state/pouches';
import { awayIn, countdownTicks, pvp, sideOf, walled } from '@/state/pvp';
import { questPlayer, quests } from '@/state/quests';
import { settings } from '@/state/settings';
import { skillBar } from '@/state/skill-bar';
import type { ShownStatus } from '@/state/statuses';
import { statuses as shownStatuses } from '@/state/statuses';
import { target } from '@/state/target';
import type { Target } from '@/state/target';
import { teleport } from '@/state/teleport';
import type { Destination } from '@/state/teleport';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

import { loadArt, Fx } from '../effects/fx';
import type { Body } from '../effects/types';
import { warnOnce } from '../effects/warn';
import { mapGround } from '../map-image';
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
import { FeedWatch } from '../systems/feed-watch';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock } from '../systems/interpolation';
import type { LookWire, RemoteLook } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { Lock } from '../systems/lock';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import type { Point } from '../systems/mob-attacks';
import { MobTracker } from '../systems/mobs';
import { PotionKeys } from '../systems/potion-keys';
import { Predictor, movementParams, wireState } from '../systems/prediction';
import type { MapKind, Mark as MoveMark, Pace } from '../systems/prediction';
import { healReach, SkillKeys } from '../systems/skill-keys';
import { moveStart, wayFor } from '../systems/skill-way';
import { relationOf } from '../systems/teams';

import { Bars } from './bars';
import { BattleMarks } from './battle-marks';
import { beatsOf } from './beats';
import type { Beats } from './beats';
import type { Figure, Gait, Lighting, Pose } from './body';
import { regionShadows, STILL, sunShadows } from './body';
import type { ChunkSource } from './chunk-build';
import { Combat, playerBody } from './combat';
import { cutMobs } from './cut-mobs';
import { Figures } from './figures';
import { FishSchools } from './fish-schools';
import { Flames } from './flames';
import { CHEST_M, cameraAt, easeShare, heldView, yawOf } from './follow';
import { Labels } from './labels';
import { LockRing } from './lock-ring';
import { drawnAs, mobLook, npcLook, playerLook } from './looks';
import type { ModelLook } from './looks';
import { groundUnder, loadMap, standsIn } from './map';
import type { RegionMap, WorldMap } from './map';
import type { ActionPose } from './model-looks';
import { Models } from './models';
import type { ModelSource } from './models';
import { Named } from './named';
import { NumberLayer } from './numbers';
import { Outline } from './outline';
import { Overlay } from './overlay';
import { Pacing } from './pacing';
import { buildPlace, fade, fogFor } from './place';
import type { Place } from './place';
import { PouchMarks } from './pouch';
import { PrepWall } from './prep-wall';
import type { RegionArea } from './region-bake';
import { buildRegion } from './region-place';
import { RoundShadows } from './round-shadows';
import { SeaBubbles } from './sea-bubbles';
import { SeaRays } from './sea-rays';
import { SEA_SUN } from './sealight';
import { ShieldBubbles } from './shield-bubbles';
import { teleportGlow } from './teleport-glow';
import { Palette } from './toon';
import { Torchlight } from './torchlight';

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
  /** Builds a region's chunks off the page's thread; the world closes it as it leaves each map. */
  readonly chunks: ChunkSource;
  /** The page's clock in ms, which the keys, actions and frames share. */
  readonly now: () => number;
  /** Reloads the page, for a world that can't go on: a region whose chunk won't come. */
  readonly reload: () => void;
}

type Wire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];
/** A living mob's mark, rewritten in place each frame. */
type LiveMark = { -readonly [K in keyof Mark]: Mark[K] };

const FADE_MS = 250;
const RETRY = { baseMs: 500, capMs: 10_000, jitter: 'equal' } as const; // a map that didn't load tries again after a backoff
const RELOAD_MS = 3000; // a broken region's notice shows this long before the page reloads
/** The farthest one input can carry the player before the hold sees them: the longest move a skill makes, past their body's radius (large-regions.md section 10). Out of a match only a mob's 0.5 m knockback pushes a player. */
const CARRY_M =
  Math.max(
    ...skills
      .flatMap((s) => [...s.stages, ...s.traits.flatMap((t) => t.stage ?? [])])
      .map((st) => st.move?.distance ?? 0),
  ) + movement.radius;
const BANNER_MS = 1800;
const STALL_MS = 2000; // 40 ticks without a frame: the world has stalled, though the socket is open
const SUN = new Vector3(0.35, -1, 0.6);

/** A map under the sun: its light, and its shadows, cascaded over a region. */
function sunLighting(scene: Scene, map: WorldMap): Lighting {
  const sun = new DirectionalLight('sun', SUN.normalizeToNew(), scene);
  return {
    palette: new Palette(scene),
    shadows: (map.region ? regionShadows : sunShadows)(sun, SHADOW_MAP),
  };
}

/** A sea region's lighting (underground-and-underwater.md section 7): its sun nearly overhead, its shadows cascaded and falling almost straight down, its toon materials cyan and softer with the caustics. */
function seaLighting(scene: Scene): Lighting {
  const sun = new DirectionalLight('sun', SEA_SUN.normalizeToNew(), scene);
  return { palette: new Palette(scene, 'sea'), shadows: regionShadows(sun, SHADOW_MAP) };
}

/** A cave region's lighting (underground-and-underwater.md section 7): no sun and so no shadows, its toon materials lit by its torches' light map; the old open-topped Cave keeps the sun. */
function caveLighting(scene: Scene, cave: RegionMap): Lighting {
  return { palette: new Palette(scene, new Torchlight(scene, cave.light)), shadows: null };
}
const SHADOW_MAP = 2048;
const TURN_MS = 60; // a turn eases most of the way in this
const MAX_FRAME_MS = 100; // a frame after a stall moves no further than this
const GLOW_SLACK_MS = 500; // the channel's glow lasts this past its length, for the MapChange to come
const MOVES: ReadonlySet<Action> = new Set(['left', 'right', 'up', 'down', 'jump']);
const MARKS_MS = 100; // how often the map views' marks move
const EDGE_FADE_M = 8; // a body fades over this much before the side edge of what every frame brings
const LOCK_INSET = 0.08; // the share of the canvas's width and height in from each edge a mob must be for the lock to pick it
const CORRECTION_MS = 100; // a reset's jump is drawn as a shove over this
const RUN_SPEED = 3; // m/s: from here a body runs rather than walks
const SPRINT_AT = sprintSteps(movement); // a body sprints from this count of sub-steps walked
const STILL_SPEED = 0.1;
const DYING_MS = 1000; // a killed mob lies this long before it goes
const NAME_LIFT_M = 0.35; // a name over its head
const MARKER_LIFT_PX = 18; // a marker over its name
const PORTAL_LABEL_M = 2.2;
const PROMPT_LIFT_PX = 42; // the talk prompt over an NPC's name and marker
const POUCH_PROMPT_M = 0.9; // the loot prompt over a pouch, above a box's lid
const ZOOM_STEP = 0.125; // a zoom key's press, or a wheel's notch, of the way from close to far
const WHEEL_PX = 800; // a wheel's pixels from close to far, so a notch of 100 px is a step
const CAST_M = 25; // another player casts a shadow within this of the camera's target; past it the shadow map spares their draws
const CAST_EDGE_M = 1; // starting at CAST_M less this, stopping at CAST_M more
const NAMED = 40; // other players named over their heads, the nearest the camera's target, besides the party and the lock (large-regions.md section 10)
const HURT_BAR_MS = 5000; // a mob's HP bar shows this long after it last lost HP, and while it's locked
const FAR_PLANE_M = 1500; // the camera sees no farther (large-regions.md section 9)

/** A figure drawn for one of the map's bodies, made in the background; its turn eases toward its facing. */
class Drawn {
  figure: Figure | null = null;
  yaw: number;
  pinned = false; // posed every frame whatever its pacing: the player's own, and the one locked
  private casting = true;
  private gone = false;
  private latest = 0; // the redraw whose figure it waits for; an older one that comes after is dropped
  private last: { x: number; y: number; z: number } | null = null;
  private posed: { p: Pose; at: number } = { p: 'up', at: 0 };
  private keyed: { cls: string; look: LookWire | undefined } | null = null;
  private readonly beat: number;
  readonly height: number; // how tall it stands, its look's
  private readonly middle: number; // how high its bounds' middle is over its feet
  private readonly reach: number; // and how far they reach round it, a hover and gear past its cylinder too

  /** as: what it's drawn as, which a redraw changes; pacing says which frames its bones are posed, sized as look. */
  constructor(
    made: Promise<Figure>,
    yaw: number,
    public as: string,
    private readonly pacing: Pacing,
    look: ModelLook,
  ) {
    this.yaw = yaw;
    this.beat = pacing.beat();
    this.height = look.height;
    this.middle = look.height / 2;
    this.reach = Math.hypot(look.height / 2, (look.width ?? look.height) / 2) + look.height / 2; // round its middle, and half its height more for a weapon or a wing swung out
    this.redraw(made, as);
  }

  /** Redraws a player when their class or what their look shows has changed; a frame's look is weighed only when it's a new one. */
  dress(cls: string, look: LookWire | undefined, made: (look: ModelLook) => Promise<Figure>): void {
    if (this.keyed?.cls === cls && this.keyed.look === look) {
      return;
    }
    this.keyed = { cls, look };
    const as = drawnAs(cls, look);
    if (as !== this.as) {
      this.redraw(made(playerLook(cls, look)), as); // a promotion or a wardrobe change
    }
  }

  /** Draws it as made from now on, once made is in, which a later redraw overtakes; till then it shows what it did. */
  redraw(made: Promise<Figure>, as: string): void {
    this.as = as;
    const mine = ++this.latest;
    made.then(
      (f) => {
        if (this.gone || mine !== this.latest) {
          f.dispose();
          return;
        }
        this.figure?.dispose();
        this.figure = f;
        f.cast(this.casting); // before any frame draws it
        if (this.posed.p !== 'up') {
          f.pose(this.posed.p, this.posed.at); // taken while it loaded
        }
      },
      () => undefined, // nothing new to draw; made() says why
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
    f.update(
      now,
      dtMs,
      gait ?? gaitAt(dtMs > 0 ? (moved * 1000) / dtMs : 0, true),
      this.pinned || this.pacing.poses(x, y + this.middle, z, this.reach, this.beat),
    );
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

  /** Casts its shadow while away, its distance from the camera's target, is within CAST_M, by a metre either way so one on the line doesn't flicker. */
  castAt(away: number): void {
    this.casting = away < CAST_M + (this.casting ? CAST_EDGE_M : -CAST_EDGE_M);
    this.figure?.cast(this.casting);
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

  /** Whether it's drawn this frame: made, and not hidden. */
  get shown(): boolean {
    return this.figure?.root.isEnabled() ?? false;
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
  sprint: [
    { pace: 'sprint', onGround: false },
    { pace: 'sprint', onGround: true },
  ],
} as const satisfies Record<Gait['pace'], readonly [Gait, Gait]>;

/** Moves the zoom setting by d, held between close and far. */
/** How far to each side every frame brings bodies on a map sizeX by sizeZ: the block's side columns, from anywhere in the player's cell; null on a map one block holds from every cell, which the server sends whole (large-regions.md section 5). */
function sideEdge(sizeX: number, sizeZ: number): number | null {
  const nx = Math.max(1, Math.ceil(sizeX / interest.cell));
  const nz = Math.max(1, Math.ceil(sizeZ / interest.cell));
  const whole = nx - 1 <= interest.side && nz - 1 <= Math.min(interest.behind, interest.ahead);
  return whole ? null : interest.side * interest.cell;
}

function zoomBy(d: number): void {
  const s = settings.getState();
  s.setZoom(Math.min(1, Math.max(0, s.zoom + d)));
}

/** A body moving at speed m/s, its sprint's count at sprint: a count at the sprint's start sprints, whatever a buff makes the walk. */
function gaitAt(speed: number, onGround: boolean, sprint = 0): Gait {
  const pace =
    speed < STILL_SPEED
      ? 'still'
      : sprint >= SPRINT_AT
        ? 'sprint'
        : speed < RUN_SPEED
          ? 'walk'
          : 'run';
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
  private roundShadows: RoundShadows | null = null; // a cave's, which has no sun
  private flames: Flames | null = null; // its torches'
  private rays: SeaRays | null = null; // a sea's
  private seaBubbles: SeaBubbles | null = null; // a sea's
  private fish: FishSchools | null = null; // a sea's
  private breathAt = 0; // the frame's moment, for the bubbles' walk over the bodies
  private pouchMarks: PouchMarks | null = null;
  private place: Place | null = null;
  private map: WorldMap | null = null;
  private combat: Combat | null = null;
  private outline: Outline | null = null;
  private ring: LockRing | null = null;
  private bubbles: ShieldBubbles | null = null;
  private prepWall: PrepWall | null = null;
  private battleMarks: BattleMarks | null = null;
  private walledUp = false; // a match's prep wall stands, and attacks and skills wait for the round
  private keptSide = 0; // in a battle past its prep, the player's side, whose geometry has the other side's base standing; 0 elsewhere
  private remoteLooks: ReadonlyMap<string, RemoteLook> = new Map(); // as drawn this frame
  private lockedAt: MapSpot | null = null; // the lock as drawn, for the map views
  private lockedId: string | null = null; // and its ref, which the place opens round
  private readonly statusTimes = new Map<string, ShownStatus & { ends: number }>(); // by unit ref and status, as the HUD times them
  private marksDue = 0;
  private sideM: number | null = null; // how far to each side every frame brings bodies, on a map cut by cells; null on one sent whole
  private readonly edgeHidden = new Set<string>(); // the refs of bodies past it this frame, drawn nowhere
  private readonly leftSight = new Map<string, { at: MapSpot; spots: PartyState['spots'] }>(); // each party member who stopped being drawn: where, and the spots the server had told then
  private readonly markedUnits = new Map<string, readonly StatusOn[]>(); // showStatuses', less those past the side edge
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
  private areaId = ''; // the region's area the server last said the player stands in; '' off a region
  private offs: (() => void)[] = [];
  private glow: (() => void) | null = null; // the teleport channel's
  private channel = 0; // the region's channel the latest Snapshot named; 0 off regions
  private readonly named = new Named(NAMED);
  private keysOff: (() => void) | null = null;
  private wheelOff: (() => void) | null = null;

  private mapId = '';
  private tickRateHz = 20;
  private timing: TickTiming = tickTiming(20, movement.dt);
  private clock = new FixedStep(this.timing);
  private renderClock = new RenderClock(this.timing.tickMs);
  private lastTick = 0;
  private feed = new FeedWatch(STALL_MS);
  private stalled = false;
  private placed = false; // set by the first frame that names the local player
  private groundHeld = false; // the region's ground round the player is still coming: their keys wait
  private heldSince = 0;
  private veiled = false; // the veil waits for what the arrival sees to be drawn
  private reloading = false;
  private latestMe: Wire | null = null; // the newest frame of the local player, which places them after a respawn here
  private myTeam = 0; // the player's side in a match as their last frame gave it, kept while they're down
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
  private readonly pacing = new Pacing();
  private zoom = settings.getState().zoom; // the zoom drawn, easing toward the setting
  private readonly lookAt = new Vector3();
  private readonly head = new Vector3();
  private remotes: RemotePlayers | null = null;
  private readonly remoteViews = new Map<string, Drawn>();
  private readonly deadRemotes = new Set<string>();
  private bossSent: MobWire | null = null; // the field boss as the frames last sent it for its bar, wherever it stands; never drawn from
  private mobs = new MobTracker(this.timing.tickMs);
  private readonly mobHurt = new Map<number, { hp: number; at: number }>(); // each mob's HP as last drawn, and when it last fell
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
    { readonly view: Drawn; readonly npc: Npc; y: number }
  >();

  private constructor(private readonly deps: WorldDeps) {
    this.sender = new InputSender({
      send: (input, tick, view) => deps.socket.sendInput(input, tick, view),
      walk: () => settings.getState().walk,
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
    w.bindWheel();
    w.load(arriving);
    return w;
  }

  /** How often the world was found elsewhere than the player was predicted, outside a new map or a rejoin. */
  get resets(): number {
    return this.resets0 + (this.predictor?.resets ?? 0);
  }

  /** What the player walks, as the world has them walk it and as the match's state says, from the next step: a match's prep wall, drawn, through prep and the count into a round (pvp.md section 3); in a battle after, their side's geometry, the other side's base standing (section 7); else the map's. */
  private setGround(predictor: Predictor): void {
    const map = this.map;
    if (!map) {
      return;
    }
    const match = pvp.getState().match;
    const up = !!map.prepWall && walled(match?.phase);
    const side = !up && map.battle ? sideOf(match, this.deps.player.characterId) : 0;
    if (up === this.walledUp && side === this.keptSide) {
      return;
    }
    if (up !== this.walledUp) {
      this.prepWall?.show(up);
    }
    this.walledUp = up;
    this.keptSide = side;
    if (up) {
      predictor.setGeometry(map.prepWall.walled);
    } else {
      predictor.setGeometry(
        map.battle && side ? map.battle.kept[side === 1 ? 0 : 1] : map.geometry,
      );
    }
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
    this.setGround(predictor);
    const match = pvp.getState().match;
    this.battleMarks?.show(
      match?.points ?? [],
      sideOf(match, this.deps.player.characterId),
      this.keptSide,
    );
    predictor.holdFor(countdownTicks(pvp.getState().match, this.timing.tickMs)); // known from prep's or the gap's end, before the count's first Delta
    if (this.placed) {
      const held = this.holdForGround(predictor.state, now);
      if (this.attackHeld) {
        this.attack(false);
      }
      for (const step of this.clock.advance(dt)) {
        if (this.transitionState.dead || this.stalled) {
          continue; // the server holds a dead player; a stalled world would take no input, so none is predicted
        }
        if (step.tickStart) {
          const view = this.renderClock.viewAt(now);
          const input = sender.tickStart(view, held); // sends it, so the socket's sequence is its own
          this.heldNow = input;
          predictor.beginTick(this.deps.socket.sequence, input, view);
        }
        this.before = predictor.state;
        predictor.subStep();
      }
    }
    this.pacing.next(
      this.follow.x,
      this.follow.z,
      scene.getRenderId() > 0 ? scene.getTransformMatrix() : null,
    );
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
    this.shadeBodies();
    this.moveCamera(dt);
    const seconds = (PrecisionDate.Now - this.deps.engine.startTime) / 1000; // the toon materials' clock for their light's flicker and the caustics
    this.flames?.frame(
      seconds,
      this.camera?.position.x ?? this.follow.x,
      this.camera?.position.z ?? this.follow.z,
    ); // its cards turned to the camera where this frame draws from
    if (this.camera) {
      this.rays?.frame(seconds, this.camera.position, this.follow);
      this.fish?.frame(seconds, this.camera.position, this.follow, this.floorAt);
    }
    this.breatheBodies(now);
    this.unveil();
    this.combat?.frame(now);
    this.closeDialogOutOfReach();
    this.reachPouches();
    this.showPrompt();
    skillBar.getState().endBuffs(now);
    if (this.camera) {
      this.overlay.frame(scene, this.camera);
      this.bars.place();
      this.labels.place();
      this.numbers.place(now);
      this.outline?.cull(this.camera);
    }
    scene.render();
  }

  dispose(): void {
    this.teardown();
    this.keysOff?.();
    this.keysOff = null;
    this.wheelOff?.();
    this.wheelOff = null;
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
    let round: RoundShadows | null;
    let flames: Flames | null;
    let rays: SeaRays | null;
    let seaBubbles: SeaBubbles | null;
    let fish: FishSchools | null;
    let figures: Figures;
    let pouchMarks: PouchMarks;
    let fx: Fx;
    try {
      const cave = map.region?.region.sky === 'cave' ? map.region : null;
      const sea = map.region?.region.sky === 'sea';
      lighting = cave
        ? caveLighting(scene, cave)
        : sea
          ? seaLighting(scene)
          : sunLighting(scene, map);
      rays = sea ? new SeaRays(scene, lighting.palette) : null;
      round = cave && new RoundShadows(scene, lighting.palette, map);
      const models = new Models(scene, this.deps.models);
      seaBubbles = sea ? new SeaBubbles(scene) : null;
      fish = sea ? new FishSchools(scene, lighting.palette, models) : null;
      flames = cave && new Flames(scene, lighting.palette, models, cave.region.lights);
      figures = new Figures(scene, lighting, models);
      pouchMarks = new PouchMarks(scene, models, lighting);
      fx = new Fx(scene, map, art, this.deps.now, lighting.palette);
      place = map.region
        ? buildRegion(scene, map.region, lighting, models, this.deps.chunks, {
            landed: () => this.landed(),
            broke: (k) => this.broke(map.id, k),
            now: this.deps.now,
          })
        : await buildPlace(scene, map, lighting, models);
    } catch (err) {
      scene.dispose();
      throw err;
    }
    if (visit !== this.visit) {
      place.dispose();
      scene.dispose();
      return;
    }
    if (arriving.mapId !== id) {
      place.dispose();
      scene.dispose();
      return this.build(arriving, visit);
    }
    this.scene = scene;
    this.lighting = lighting;
    this.roundShadows = round;
    this.flames = flames;
    this.rays = rays;
    this.seaBubbles = seaBubbles;
    this.fish = fish;
    this.figures = figures;
    this.pouchMarks = pouchMarks;
    this.place = place;
    this.map = map;
    this.camera = new TargetCamera('camera', Vector3.Zero(), scene);
    this.camera.maxZ = FAR_PLANE_M;
    fogFor(scene, map.sky, this.zoom);
    this.outline = new Outline(scene);
    this.ring = new LockRing(scene, map, lighting.palette);
    this.bubbles = new ShieldBubbles(scene);
    this.prepWall = map.prepWall && new PrepWall(scene, map.prepWall.boxes, map.geometry.terrain);
    this.battleMarks =
      map.battle &&
      new BattleMarks(scene, map, map.battle.points, map.battle.bases, lighting.palette);
    fx.warm(); // every look's shaders built before a fight needs them
    mapView
      .getState()
      .showMap(map.id, [map.geometry.size_x, map.geometry.size_z], mapGround(map), map.waypoints);
    this.start(arriving, fx);
  }

  /** Starts the built map: its clocks and trackers, the player hidden until their frame, the NPCs and portal labels, and the socket's frames. */
  private start(arriving: Arrival, fx: Fx): void {
    const map = this.map!;
    const socket = this.deps.socket;
    const { characterId } = this.deps.player;
    this.mapId = map.id;
    this.sideM = sideEdge(map.geometry.size_x, map.geometry.size_z);
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
    this.me = this.drawn(playerLook(this.meCls, undefined), at?.yaw ?? 0, this.meCls); // dressed by its first frame
    this.me.pinned = true;
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
    this.showPortalLabels();
    const m = maps.find((x) => x.id === map.id);
    if (m) {
      this.banner(m.min_level !== null ? `${m.name} · Lv ${m.min_level}–${m.max_level}` : m.name); // a region's area's follows with its Snapshot
    }
    this.pouchMarks?.show(pouches.getState().pouches);
    this.offs.push(
      pouches.subscribe((now, before) => {
        if (now.pouches !== before.pouches) {
          this.pouchMarks?.show(now.pouches);
        }
      }),
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
          this.dressMe();
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
      socket.on('areaEntered', (m) => this.onWorld(m)),
      socket.on('damage', (m) => {
        this.combat?.damage(m.d, m.tick);
        this.guardedBy(m.d);
        const me = `p:${this.deps.player.characterId}`;
        if (m.d.source === me || m.d.target === me) {
          this.predictor?.fought(m.tick, ticksIn(actionData.combat_ms, this.timing.tickMs));
        }
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
    this.veiled = true;
    this.unveil();
  }

  /** Lifts the veil once what the camera's target sees is drawn: at once on a map, a region's chunks round it first. */
  private unveil(): void {
    if (this.veiled && this.place?.ready(this.follow.x, this.follow.z)) {
      this.veiled = false;
      this.veil.style.opacity = '0';
    }
  }

  /** Whether the player's keys wait for the region's ground: while a chunk is missing within the farthest one input could carry them, a move under way included, and its solids' reach past that, as they travel with their centre's chunk (large-regions.md section 10). Every arrival meets it, a walk to the ground's edge too. */
  private holdForGround(s: State, now: number): boolean {
    const region = this.map?.region;
    const held = !!region && !region.ground.covers(s.x, s.z, CARRY_M + region.region.solids.reach);
    if (held !== this.groundHeld) {
      this.groundHeld = held;
      connection.getState().setGroundLoading(held);
      if (held) {
        this.heldSince = now;
        console.info(
          `finga: the keys wait for the ground round (${s.x.toFixed(1)}, ${s.z.toFixed(1)})`,
        );
      } else {
        console.info(`finga: the ground came after ${Math.round(now - this.heldSince)} ms`);
      }
    }
    return held;
  }

  /** A region's chunk failed every try: says so, and reloads the page, which fetches the bake afresh. */
  private broke(mapId: string, chunk: string): void {
    if (this.reloading) {
      return;
    }
    this.reloading = true;
    console.info(`finga: chunk ${chunk} of ${mapId} won't load; reloading`);
    toasts.getState().push('The map’s ground didn’t load. Reloading…');
    setTimeout(() => this.deps.reload(), RELOAD_MS);
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

  /** The wheel over the world zooms the camera, in place of scrolling the page: by its pixels, or a step a notch where it counts lines or pages. */
  private bindWheel(): void {
    const parent = this.deps.parent;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      zoomBy(
        e.deltaMode === WheelEvent.DOM_DELTA_PIXEL
          ? e.deltaY / WHEEL_PX
          : Math.sign(e.deltaY) * ZOOM_STEP,
      );
    };
    parent.addEventListener('wheel', wheel, { passive: false });
    this.wheelOff = () => parent.removeEventListener('wheel', wheel);
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
      case 'walk': {
        const walk = !settings.getState().walk;
        settings.getState().setWalk(walk); // the next input carries it; saved with the zoom
        toasts.getState().push(walk ? 'Walk' : 'Sprint', 'info');
        return;
      }
      case 'zoomIn':
        zoomBy(-ZOOM_STEP);
        return;
      case 'zoomOut':
        zoomBy(ZOOM_STEP);
        return;
      case 'talk':
        this.talk();
        return;
      case 'loot':
        this.loot();
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
    return this.placed && !this.stalled && !this.groundHeld && !this.transitionState.dead;
  }

  private onWorld(m: Held): void {
    this.feed.heard();
    switch (m.t) {
      case 'snapshot':
        this.channel = m.d.channel;
        mapView.getState().inChannel(m.d.channel); // with the map it's of, which a teardown cleared
        this.enteredArea(m.d.area);
        this.onSnapshot(m.d.players, m.d.mobs, m.d.boss, this.tickOf(m.tick));
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
      case 'areaEntered':
        this.enteredArea(m.d.area);
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
    boss: MobWire | undefined,
    tick: number,
  ): void {
    const now = this.deps.now();
    const t = tick * this.timing.tickMs;
    this.renderClock.update(tick, now);
    this.bossSent = boss ?? null;
    this.remotes?.applySnapshot(t, players);
    this.mobs.applySnapshot(t, mobs);
    this.deadRemotes.clear();
    for (const p of players) {
      if (p.dead && p.id !== this.deps.player.characterId) {
        this.deadRemotes.add(p.id);
      }
    }
    const me = players.find((p) => p.id === this.deps.player.characterId);
    if (me) {
      this.latestMe = me;
      this.dressMe();
      this.myTeam = me.team;
      this.placeAt(me);
      this.flash.set(me.invulnerable, now);
    }
  }

  private onDelta(d: Data<'delta'>, tick: number): void {
    const now = this.deps.now();
    this.ackSeq = d.ackSeq;
    const t = tick * this.timing.tickMs;
    this.renderClock.update(tick, now);
    const unseen = d.unseen;
    const removedPlayers = unseen ? [...d.removedPlayers, ...unseen.players] : d.removedPlayers; // out of sight, a player goes as one who left
    this.remotes?.applyDelta(t, d.players, removedPlayers);
    this.mobs.applyDelta(t, d.mobs, d.removedMobs, unseen?.mobs ?? []);
    if (d.boss) {
      this.bossSent = d.boss;
    }
    const leaving = new Set(removedPlayers);
    for (const p of d.players) {
      if (p.id === this.deps.player.characterId) {
        continue;
      }
      if (p.dead) {
        this.deadRemotes.add(p.id);
      } else if (!leaving.has(p.id)) {
        this.deadRemotes.delete(p.id); // one leaving keeps its last pose till its figure goes, drawn behind the frame
      }
    }
    const me = d.players.find((p) => p.id === this.deps.player.characterId);
    if (!me || !this.predictor) {
      return;
    }
    const acting = !this.restrained().act;
    this.latestMe = me;
    this.dressMe();
    this.myTeam = me.team;
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
    me.draw(x, y, z, aim, now, dtMs, gaitAt(speed, b.on_ground, b.sprint));
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
    this.edgeHidden.clear(); // drawing the remotes and the mobs fills it again
    const remotes = this.remotes;
    const renderTime = this.renderClock.renderTime(now);
    if (!remotes || renderTime === null) {
      return;
    }
    const { shown, looks, gone } = remotes.render(renderTime);
    this.named.pick(shown, this.follow);
    const members = party.getState().party;
    this.remoteLooks = looks;
    let changed = gone.length > 0;
    for (const id of gone) {
      const at = this.remoteViews.get(id)?.at;
      if (at && this.sideM !== null && isMember(party.getState().party, id)) {
        this.leftSight.set(id, { at: { x: at.x, z: at.z }, spots: party.getState().spots });
      }
      this.remoteViews.get(id)?.dispose();
      this.remoteViews.delete(id);
      this.deadRemotes.delete(id);
      this.labels.delete(`p:${id}`);
      this.remoteAims.delete(id);
      this.combat?.forget(id);
    }
    const away = awayIn(pvp.getState().match);
    for (const [id, s] of shown) {
      let v = this.remoteViews.get(id);
      const cls = remotes.classOf(id);
      const look = remotes.lookOf(id);
      if (!v) {
        v = this.drawn(playerLook(cls, look), yawOf(s.hx, s.hz), drawnAs(cls, look));
        this.remoteViews.set(id, v);
        changed = true;
      }
      v.dress(cls, look, this.dressing);
      v.pinned = this.lockedId === `p:${id}`;
      const aim = this.remoteAims.get(id);
      v.draw(
        s.x,
        s.y,
        s.z,
        aim && now < aim.until ? aim.yaw : yawOf(s.hx, s.hz),
        now,
        dtMs,
        gaitAt(Math.hypot(s.vx, s.vz), s.on_ground, s.sprint),
      );
      v.pose(this.deadRemotes.has(id) ? 'dead' : 'up', now);
      const edge = this.edgeOf(`p:${id}`, s.x);
      v.show(edge !== 'hidden');
      v.fade(edge === 'faded' || (looks.get(id)?.dodging ?? false));
      v.castAt(Math.hypot(s.x - this.follow.x, s.z - this.follow.z));
      if (
        edge === 'hidden' ||
        !(this.named.has(id) || isMember(members, id) || this.lockedId === `p:${id}`)
      ) {
        this.labels.delete(`p:${id}`);
        continue;
      }
      const info = remotes.infoOf(id);
      const side = relationOf(this.myTeam, info?.team ?? 0);
      this.labels.set(
        `p:${id}`,
        side === 'none' ? 'player' : side,
        remotes.nameOf(id),
        s.x,
        s.y + movement.height + NAME_LIFT_M,
        s.z,
        0,
        info?.bot ? 'BOT' : away.has(id) ? 'Reconnecting' : '',
      );
    }
    if (changed) {
      nearby
        .getState()
        .setNearby([...this.remoteViews.keys()].map((id) => ({ id, name: remotes.nameOf(id) })));
    }
  }

  /** How body ref at x is drawn across from the player: whole within the distance every frame brings, faded over its last EDGE_FADE_M, and not at all past it, where it comes and goes with the player's cell (large-regions.md section 5). */
  private edgeOf(ref: string, x: number): 'whole' | 'faded' | 'hidden' {
    const across = Math.abs(x - this.follow.x);
    const edge =
      this.sideM === null || across < this.sideM - EDGE_FADE_M
        ? 'whole'
        : across <= this.sideM
          ? 'faded'
          : 'hidden';
    if (edge === 'hidden') {
      this.edgeHidden.add(ref);
    }
    return edge;
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
    const { shown, gone, unseen } = this.mobs.render(renderTime);
    const carried = this.combat?.mobsDrawn(renderTime, this.playerTime(renderTime), now, shown);
    for (const id of gone) {
      const type = this.mobTypes.get(id);
      const v = this.forgetMob(id);
      if (v) {
        v.pose('dead', now);
        this.dying.push({ id, type, view: v, until: now + DYING_MS });
      }
    }
    for (const id of unseen) {
      this.forgetMob(id)?.dispose(); // out of sight: it goes as it is
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
      const edge = this.edgeOf(mark.id, s.x);
      mark.inView = edge !== 'hidden' && this.overlay.within(s.x, middle, s.z, LOCK_INSET);
      mark.inFrame = edge !== 'hidden' && this.overlay.within(s.x, middle, s.z, 0);
      const hurt = this.mobHurt.get(id);
      if (!hurt) {
        this.mobHurt.set(id, { hp: s.hp, at: -Infinity });
      } else if (s.hp !== hurt.hp) {
        hurt.at = s.hp < hurt.hp ? now : hurt.at;
        hurt.hp = s.hp;
      }
      const barred = now - (hurt?.at ?? -Infinity) < HURT_BAR_MS || this.lockedId === mark.id;
      if (m && !m.boss && s.hp > 0 && edge !== 'hidden' && barred) {
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
      v.pinned = this.lockedId === mark.id;
      v.draw(s.x, s.y, s.z, s.yaw, now, dtMs);
      v.pose(poseOf(s.pose), now - s.poseMs);
      v.show(edge !== 'hidden');
      v.fade(edge === 'faded');
    }
    this.marks.length = this.markFoes(marked);
    const boss = fieldBoss.getState().boss;
    const drawn = boss && shown.get(boss.mobId);
    const sent = boss && this.bossSent?.id === boss.mobId ? this.bossSent : null;
    const hp = drawn ?? sent; // the boss bar shows what is drawn, else the boss as the frames send it from anywhere on the map
    fieldBoss.getState().setHp(hp ? { hp: hp.hp, maxHp: hp.maxHp } : null);
  }

  /** Stops drawing mob id: its figure, which it returns, its type and its bar. */
  private forgetMob(id: number): Drawn | undefined {
    const v = this.mobViews.get(id);
    this.mobViews.delete(id);
    this.mobTypes.delete(id);
    this.bars.delete(id);
    this.mobHurt.delete(id);
    return v;
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
    const mine = this.myTeam;
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

  /** A cave's round shadows this frame, under each body drawn near the camera's target: the player, the others, the mobs, the dying and the NPCs. */
  private shadeBodies(): void {
    const round = this.roundShadows;
    if (!round) {
      return;
    }
    round.begin(this.follow.x, this.follow.z);
    if (this.me) {
      this.shade(this.me, movement.radius);
    }
    this.remoteViews.forEach(this.shadePlayer);
    this.mobViews.forEach(this.shadeMob);
    this.dying.forEach(this.shadeDying);
    this.npcViews.forEach(this.shadeNpc);
    round.end();
  }

  /** A sea's bubbles this frame: its drawn vents near the camera's target stream, and each body drawn near it, the dying aside, breathes out a few now and then. */
  private breatheBodies(now: number): void {
    const bubbles = this.seaBubbles;
    if (!bubbles || !this.place) {
      return;
    }
    bubbles.vents(this.follow.x, this.follow.z, this.place.vents());
    this.breathAt = now;
    if (this.me) {
      this.breathe(this.me);
    }
    this.remoteViews.forEach(this.breathe);
    this.mobViews.forEach(this.breathe);
    this.npcViews.forEach(this.breatheNpc);
  }

  // made once, so a frame's walk over the maps makes nothing
  private readonly breathe = (v: Drawn) => {
    const at = v.at;
    if (at && v.shown) {
      this.seaBubbles?.breathe(
        v,
        at.x,
        at.y + v.height,
        at.z,
        this.follow.x,
        this.follow.z,
        this.breathAt,
      );
    }
  };
  private readonly breatheNpc = (n: { readonly view: Drawn }) => this.breathe(n.view);
  private readonly floorAt = (x: number, z: number) => groundUnder(this.map!, x, z);

  /** v's round shadow, a body of radius r, if it's drawn: under its feet, on a solid's top it stands on too. */
  private shade(v: Drawn, r: number): void {
    const at = v.at;
    if (at && v.shown) {
      this.roundShadows?.add(at.x, at.y, at.z, r);
    }
  }

  // made once, so a frame's walk over the maps makes nothing
  private readonly shadePlayer = (v: Drawn) => this.shade(v, movement.radius);
  private readonly shadeMob = (v: Drawn, id: number) =>
    this.shade(v, mobById(this.mobTypes.get(id))?.radius ?? movement.radius);
  private readonly shadeDying = (d: { readonly view: Drawn; readonly type: string | undefined }) =>
    this.shade(d.view, mobById(d.type)?.radius ?? movement.radius);
  private readonly shadeNpc = (n: { readonly view: Drawn }) => this.shade(n.view, movement.radius);

  /** Eases the camera toward the player's chest by the time passed, held so the map's edge stops it, fades what stands between them, and opens the place round the chest and the mobs fighting them. */
  private moveCamera(dtMs: number): void {
    const camera = this.camera;
    const map = this.map;
    const at = this.me?.at;
    if (!camera || !map || !at || !this.placed) {
      return;
    }
    const share = easeShare(dtMs);
    const zoom = settings.getState().zoom;
    if (this.zoom !== zoom) {
      this.zoom = Math.abs(zoom - this.zoom) < 1e-3 ? zoom : this.zoom + (zoom - this.zoom) * share;
      fogFor(this.scene!, map.sky, this.zoom);
    }
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
      this.zoom,
      this.lookAt,
    );
    cameraAt(look.x, look.y, look.z, this.zoom, camera.position);
    camera.setTarget(look);
    if (this.place) {
      this.place.follow(look.x, look.z);
      const head = this.head.set(at.x, at.y + movement.height, at.z);
      fade(camera.position, head, this.place.looks.around(camera.position, head), dtMs);
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
      if (
        v.figure &&
        (this.remoteLooks.get(id)?.shield ?? 0) > 0 &&
        !this.edgeHidden.has(`p:${id}`)
      ) {
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

  /** Redraws the player when their class or what their newest frame says they wear has changed. */
  private dressMe(): void {
    this.me?.dress(this.meCls, this.latestMe?.look, this.dressing);
  }

  private readonly dressing = (look: ModelLook) => this.made(look);

  /** A figure for look, drawn as as, made in the background. */
  private drawn(look: ModelLook, yaw: number, as = look.file): Drawn {
    return new Drawn(this.made(look), yaw, as, this.pacing, look);
  }

  /** A figure for look; a model that fails to load, but for its map going first, says why once. */
  private made(look: ModelLook): Promise<Figure> {
    const { figures, scene } = this;
    const made = figures ? figures.make(look) : Promise.reject(new Error('no scene'));
    made.catch((err: unknown) => {
      if (scene && !scene.isDisposed) {
        warnOnce(
          `${look.file} didn't load (${err instanceof Error ? err.message : String(err)}), so it isn't drawn`,
        );
      }
    });
    return made;
  }

  /** The player's class: the store's, which a promotion changes, else the one the world was entered with. */
  private get cls(): string {
    return character.getState().character?.class ?? this.deps.player.cls;
  }

  private kind(): MapKind {
    return maps.find((m) => m.id === this.mapId)?.kind ?? 'town';
  }

  /** The region's area the player stands in, from its bake; undefined off a region. */
  private area(): RegionArea | undefined {
    return this.map?.region?.region.areas.find((a) => a.id === this.areaId);
  }

  /** Whether the player stands where nobody fights: a town map, or a region's town area. */
  private inTown(): boolean {
    return this.kind() === 'town' || this.area()?.kind === 'town';
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
    let marked: ReadonlyMap<string, readonly StatusOn[]> = units;
    if (this.edgeHidden.size > 0) {
      this.markedUnits.clear();
      for (const [ref, on] of units) {
        if (!this.edgeHidden.has(ref)) {
          this.markedUnits.set(ref, on);
        }
      }
      marked = this.markedUnits;
    }
    this.combat?.statuses(marked);
    this.publishStatuses(units, now);
  }

  /** The HUD's statuses: the player's own, the locked mob's, and each party member's on this map, each timed from when it first showed, or restarted, to its end tick; and in a match, each fighter's for their frame, with their level and HP. */
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
    const here: Record<string, ShownStatus[] | null> = {};
    for (const m of party.getState().party?.members ?? []) {
      const known = m.characterId === me ? null : this.mateAt(m);
      if (known) {
        here[m.characterId] = known.drawn ? timed(`p:${m.characterId}`) : null; // out of sight: the frames don't bring theirs
      }
    }
    shownStatuses.getState().show({
      me: timed(`p:${me}`),
      target: this.lockedId === null ? [] : timed(this.lockedId),
      party: here,
    });
    const fought: Record<string, FighterLive> = {};
    for (const f of pvp.getState().match?.sides.flat() ?? []) {
      const p = this.remotes?.infoOf(f.id); // never the player, who isn't among the remotes
      if (p) {
        const down = this.deadRemotes.has(f.id);
        fought[f.id] = {
          level: p.level,
          hp: down ? 0 : p.hp,
          maxHp: p.maxHp,
          down,
          statuses: timed(`p:${f.id}`),
        };
      }
    }
    fighters.getState().show(fought);
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
      s && !this.inTown() && this.free() ? this.lock.update(s.x, s.z, this.marks) : null;
    const ref = locked ? parseRef(locked.id) : undefined;
    const view = ref && this.drawnOf(ref);
    const at = view?.at;
    const height =
      ref?.kind === 'mob' ? mobById(this.mobTypes.get(ref.id))?.height : movement.height;
    this.outline?.mark(
      locked && view?.figure
        ? { figure: view.figure, height: height ?? movement.height, radius: locked.radius }
        : null,
    );
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
    const { party: joined, spots } = party.getState();
    const members = joined?.members ?? [];
    for (const [id, left] of this.leftSight) {
      if (left.spots !== spots) {
        this.leftSight.delete(id); // the server has spoken since, a second at most
      }
    }
    const boss = fieldBoss.getState().boss;
    const bossAt = boss ? this.mobViews.get(boss.mobId)?.at : undefined;
    mapView.getState().mark({
      me: me && s ? { x: me.x, z: me.z, yaw: yawOf(s.hx, s.hz) } : null,
      party: members.flatMap((m) => {
        const at = m.characterId === this.deps.player.characterId ? null : this.mateAt(m)?.at;
        return at ? [{ id: m.characterId, name: m.name, x: at.x, z: at.z }] : [];
      }),
      lock: this.lockedAt,
      boss: bossAt ? { x: bossAt.x, z: bossAt.z } : null,
    });
  }

  /** Where party member m stands here as far as the player knows: drawn; or, on a map cut by cells, online and out of sight where the server's latest spots list them, or where last drawn till it next speaks (large-regions.md section 5). */
  private mateAt(m: PartyView['members'][number]): { at: MapSpot; drawn: boolean } | null {
    const drawn = this.remoteViews.get(m.characterId)?.at;
    if (drawn) {
      return { at: drawn, drawn: true };
    }
    if (!m.online || m.mapId !== this.mapId || m.channel !== this.channel) {
      return null;
    }
    const spots = party.getState().spots;
    const left = this.leftSight.get(m.characterId);
    const at = spots[m.characterId] ?? (left?.spots === spots ? left.at : undefined);
    return at ? { at, drawn: false } : null;
  }

  /** Tab: locks the next mob or enemy farther than the locked one, on a map with combat while the player may act. */
  private nextTarget(): void {
    const s = this.predictor?.state;
    if (s && !this.inTown() && this.free()) {
      this.lock.next(s.x, s.z, this.marks);
    }
  }

  /** A fresh press or a held key attacks as soon as an attack may start: at a cancelable skill's cancel point, or as the running action ends; a held key never goes ahead of a press waiting for its moment. */
  private attack(fresh: boolean): void {
    const clock = this.actions;
    if (this.inTown() || this.walledUp || !this.free() || !clock || this.restrained().act) {
      return; // the server runs no combat in towns or before a round, and drops an attack a status stops
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
    const seq = this.deps.socket.sequence;
    if (!this.deps.socket.sendAction('attack', { aim, target })) {
      return;
    }
    this.predictor?.mark(seq, endSprint); // the world ends the sprint's count from the input after the press
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
      inTown: () => this.inTown(),
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
      use: (skill, root) => {
        this.predictor?.mark(socket.sequence, endSprint); // the press, sent, ends the sprint's count from its next input, as the world ends it
        return socket.request(
          'useSkill',
          {
            skill,
            chainRoot: root,
            aim: this.myAim?.yaw ?? 0,
            target: this.myAim?.target ?? '',
            ...(this.myWay && { way: this.myWay }),
          },
          socket.sequence,
        ); // the aim, lock and way its begin just took
      },
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
  private channelGlow(fx: Fx, to: Destination | null): void {
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
          m.channel === this.channel &&
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

  /** The loot key opens the loot window on the player's pouches within reach; while it's open, it takes every item in them. */
  private loot(): void {
    const s = pouches.getState();
    if (!this.free() || s.inReach.length === 0) {
      return;
    }
    if (!ui.getState().windows.includes('loot')) {
      ui.getState().openWindow('loot');
      return;
    }
    void s.take(picksInReach(s), (picks) =>
      this.deps.socket.request('takeLoot', { picks: [...picks] }),
    );
  }

  /** How far the player's middle is from a pouch, as the server measures loot reach. */
  private reachToPouch(x: number, y: number, z: number, p: State): number {
    return Math.hypot(x - p.x, y - (p.y + movement.height / 2), z - p.z);
  }

  /** The player's pouches within reach, by id, for the window and the prompt; the window closes once none is, or the player died. */
  private reachPouches(): void {
    const p = this.predictor?.state;
    const inReach =
      p && this.placed && !this.transitionState.dead
        ? pouches
            .getState()
            .pouches.filter((o) => this.reachToPouch(o.x, o.y, o.z, p) <= lootData.reach)
            .map((o) => o.id)
        : [];
    pouches.getState().setInReach(inReach);
    if (inReach.length === 0 && ui.getState().windows.includes('loot')) {
      ui.getState().closeWindow('loot');
    }
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
    this.showLootPrompt(p);
  }

  /** The loot key's prompt over the nearest pouch in reach while the loot window is shut. */
  private showLootPrompt(p: State | undefined): void {
    const s = pouches.getState();
    const near =
      p && this.free() && !ui.getState().windows.includes('loot')
        ? s.pouches
            .filter((o) => s.inReach.includes(o.id))
            .map((o) => ({ o, d: this.reachToPouch(o.x, o.y, o.z, p) }))
            .sort((a, b) => a.d - b.d)[0]?.o
        : undefined;
    if (!near) {
      this.labels.set('loot-prompt', 'prompt', '', 0, 0, 0);
      return;
    }
    const key = keyOf(
      keyTable(settings.getState().keys),
      'loot',
      keyboardLayout.getState().printed,
    );
    this.labels.set(
      'loot-prompt',
      'prompt',
      `${key} Loot`,
      near.x,
      near.y + POUCH_PROMPT_M,
      near.z,
    );
  }

  /** Stands the NPCs, their marks and the portals' labels on a region's ground that came after they were placed. */
  private landed(): void {
    const map = this.map;
    if (!map) {
      return;
    }
    for (const n of this.npcViews.values()) {
      n.y = groundUnder(map, n.npc.x, n.npc.z);
    }
    this.showMarkers();
    this.showPortalLabels();
  }

  /** Each portal's label, over its ground, naming the map it leads to. */
  private showPortalLabels(): void {
    const map = this.map!;
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

  /** Into a region's area, on a join or a step: its banner, its name and a hunting area's levels. */
  private enteredArea(id: string): void {
    this.areaId = id;
    const a = this.area();
    if (a) {
      this.banner(a.kind === 'hunting' ? `${a.name} · Lv ${a.levels[0]}–${a.levels[1]}` : a.name);
    }
  }

  /** The text along the top a while, as a map or an area is entered. */
  private banner(text: string): void {
    const el = document.createElement('div');
    el.style.cssText = `position:absolute;left:0;right:0;top:28px;text-align:center;color:#f4f4f8;font:600 22px sans-serif;text-shadow:0 0 4px #0a0c16;pointer-events:none;transition:opacity ${2 * FADE_MS}ms`;
    el.textContent = text;
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
    this.predictor?.endFight();
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
        this.myDodge = null;
        this.actions?.reset(); // a round's reset gets up the one still standing too, mid-swing or mid-dodge (pvp.md section 7)
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
    this.mobHurt.clear();
    this.marks.length = 0;
    for (const d of this.dying) {
      d.view.dispose();
    }
    this.dying.length = 0;
    this.pouchMarks?.dispose();
    pouches.getState().setInReach([]);
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
    this.deps.chunks.close(); // the next region starts them again
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
    this.battleMarks?.dispose();
    this.battleMarks = null;
    this.walledUp = false;
    this.keptSide = 0;
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
    if (this.groundHeld) {
      this.groundHeld = false;
      connection.getState().setGroundLoading(false);
    }
    this.camera = null;
    this.place?.dispose(); // a region's chunk work and stones' listener outlive the scene
    this.place = null;
    this.figures = null;
    this.roundShadows = null;
    this.flames = null;
    this.rays = null;
    this.seaBubbles = null;
    this.fish = null;
    this.pouchMarks = null;
    this.lighting = null;
    this.map = null;
    this.scene?.dispose();
    this.scene = null;
  }
}
