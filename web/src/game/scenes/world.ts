import Phaser from 'phaser';

import { classes } from '@/gamedata/classes';
import { mobs } from '@/gamedata/mobs';
import { movement } from '@/gamedata/movement';
import { skillKeys } from '@/input/actions';
import type { Action } from '@/input/actions';
import { isTyping } from '@/input/typing';
import { FixedStep, ticksIn, tickTiming } from '@/lib/fixed-step';
import type { TickTiming } from '@/lib/fixed-step';
import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import { actionPoints } from '@/progression/actions';
import { inReach, markerFor, nearestInReach, npcById, npcsOn } from '@/progression/quests';
import { bonusAt, moveSpeedWithout, skillById } from '@/progression/skills';
import type { Skill, Stage } from '@/progression/skills';
import { moveSpeedWith } from '@/progression/stats';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import type { MapView } from '@/sim/map';
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

import { DEPTH } from '../depth';
import { colorOf } from '../effects/colors';
import { arrival } from '../effects/draw';
import type { Shot } from '../effects/draw';
import { attackEffect, damageType, hitEffects, mobEffect, skillEffect } from '../effects/ids';
import type { Damage, EffectId } from '../effects/ids';
import { shotRange } from '../effects/looks';
import { play } from '../effects/play';
import type { Body, PlayOptions, Stop } from '../effects/types';
import { beatsOf } from '../entities/beats';
import type { Beats } from '../entities/beats';
import { DamageNumberLayer } from '../entities/damage-number';
import { DropView } from '../entities/drop';
import { MobView, mobArt } from '../entities/mob';
import { OUTLINE, stackTags, tagBox } from '../entities/name-tag';
import type { TagBox } from '../entities/name-tag';
import { NpcView } from '../entities/npc';
import { PlayerView } from '../entities/player';
import { ART_SCALE, SCENES, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import { ActionClock, attackIn, attackOf, attackTiming, skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';
import { Arrival } from '../systems/arrival';
import type { Held, Spot } from '../systems/arrival';
import {
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  numberFor,
  parseRef,
} from '../systems/combat-view';
import type { Ref } from '../systems/combat-view';
import { DropTracker, departure } from '../systems/drops';
import type { DropChanges, Point } from '../systems/drops';
import { FeedWatch } from '../systems/feed-watch';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import type { AttacksDrawn, SpitDrawn, Swing } from '../systems/mob-attacks';
import { IMPACT_MS, MobAttacks } from '../systems/mob-attacks';
import { PortalSystem, standsIn } from '../systems/portals';
import { PotionKeys } from '../systems/potion-keys';
import { Predictor, movementParams, wireState } from '../systems/prediction';
import type { Pace } from '../systems/prediction';
import { SkillKeys, healReach } from '../systems/skill-keys';

import {
  bannerLines,
  belowFloor,
  groundStyle,
  mapTiles,
  portalLabel,
  portalLabelY,
} from './world-layout';

/** What the game enters the world with, from Welcome. */
export interface WorldEntry {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
  /** The world's ticks per second. */
  readonly tickRateHz: number;
}

type Player = Omit<WorldEntry, 'mapId' | 'tickRateHz'>;

export interface WorldData extends Player {
  /** The game's visit to the world this scene belongs to; a restart Phaser still runs after the app left builds nothing. */
  readonly visit: number;
  /** The map being entered: it names the map and its tick rate, and holds what came before the scene was built. */
  readonly arriving: Arrival;
}

const FADE_MS = 250;
const BANNER_MS = 1800;
const STALL_MS = 2000; // 40 ticks without a frame: the world has stalled, though the socket is open

type Wire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];
type DropWire = Data<'snapshot'>['drops'][number];

const BODY_MID_PX = movement.height / 2; // a drop flies into its owner's middle

/** An action's effect as it plays: its targets, "p:<id>" or "m:<id>", which its Damage events add to till its cancel point. */
interface Showing {
  readonly stop: Stop;
  readonly targets: string[];
  readonly open: boolean;
  readonly until: number;
}

/** A hit to show at `at`: its damage type as its source then acted, the beats of the shot it came by, and its target as last drawn. */
interface Landing {
  readonly d: Data<'damage'>;
  readonly target: Ref;
  readonly damage: Damage;
  readonly beats?: Beats;
  readonly at: number;
  last: Body | undefined;
}

export class World extends Phaser.Scene {
  private entry: Omit<WorldData, 'arriving'> = {
    characterId: '',
    cls: classes[0].id,
    name: '',
    visit: 0,
  };
  private mapId = 'town';
  private tickRateHz = 20;
  /** Let go of in teardown: the map being entered until built, then the next map while this scene fades out. */
  private arriving: Arrival | null = null;
  private timing: TickTiming = tickTiming(20, movement.dt);
  private player: PlayerView | null = null;
  private background: Phaser.GameObjects.Image | null = null;
  /** The ground under the map's floor, in a window taller than the map. */
  private underFloor: Phaser.GameObjects.TileSprite | null = null;
  private offs: (() => void)[] = [];

  private socket: GameSocket | null = null;
  private predictor: Predictor | null = null;
  private sender: InputSender | null = null;
  private clock = new FixedStep(this.timing);
  private placed = false; // set by the first Snapshot that names the local player
  /** No frame for STALL_MS: no prediction, input or actions until frames come back, so nothing snaps back. */
  private feed = new FeedWatch(STALL_MS);
  private stalled = false;

  private remotes: RemotePlayers | null = null;
  private readonly remoteViews = new Map<string, PlayerView>();
  private renderClock = new RenderClock(this.timing.tickMs);
  private lastTick = 0;

  private mobs = new MobTracker(this.timing.tickMs);
  private attacks = new MobAttacks(this.timing.tickMs, []);
  private spits: readonly SpitDrawn[] = []; // the mobs' spits in flight, as drawn this frame
  /** Each mob's attack look still playing, with the swing it shows. */
  private readonly mobLooks = new Map<number, { readonly swing: Swing; readonly stop: Stop }>();
  private readonly mobViews = new Map<number, MobView>();
  private readonly numbers = new DamageNumbers();
  private numberLayer: DamageNumberLayer | null = null;
  private actions: ActionClock | null = null;
  /** The attack key is down: it attacks again at each action's end. */
  private attackHeld = false;
  /** Each caster's action effect still showing, which their next action takes away. */
  private readonly effects = new Map<string, Showing>();
  private readonly shots = new Map<string, Shot>(); // each player's latest action, while it's a shot that flies: the Damage it brings shows as it lands
  private landings: Landing[] = [];
  /** Each player's latest action's damage type, by "p:<id>", which their hits spark in. */
  private readonly lastDamage = new Map<string, Damage>();
  private potionKeys: PotionKeys | null = null;
  private skillKeys: SkillKeys | null = null;
  private readonly flash = new InvulnFlash();
  private transitionState: TransitionState = initialTransition;

  private readonly drops = new DropTracker();
  private readonly dropViews = new Map<number, DropView>();

  private view: MapView | null = null;
  private portals: PortalSystem | null = null;
  private readonly npcViews = new Map<string, NpcView>();
  /** The portal labels, which never move. */
  private portalTags: TagBox[] = [];
  /** What players' tags keep clear of: the portal labels, and the NPCs' tags and markers as showMarkers last left them. */
  private fixedTags: TagBox[] = [];

  constructor() {
    super(SCENES.World);
  }

  /** The player's class: the store's, which a promotion changes, else the one the world was entered with. */
  private get cls(): string {
    return character.getState().character?.class ?? this.entry.cls;
  }

  init(data: WorldData): void {
    const { arriving, ...entry } = data;
    this.entry = entry;
    this.arriving = arriving;
    this.placed = false;
    this.transitionState = initialTransition;
  }

  create(data: WorldData): void {
    this.events.once(Phaser.Scenes.Events.SHUTDOWN, () => this.teardown());
    if (this.registry.get(REGISTRY.visit) !== data.visit) {
      this.scene.stop(); // its teardown lets go of the Arrival
      return;
    }
    if (this.registry.get(REGISTRY.assetsReady) === true) {
      this.build(data.arriving);
      return;
    }
    const onReady = () => this.build(data.arriving);
    this.game.events.once(EVENTS.assetsReady, onReady);
    this.offs.push(() => this.game.events.off(EVENTS.assetsReady, onReady));
  }

  private build(arriving: Arrival): void {
    this.mapId = arriving.mapId;
    this.tickRateHz = arriving.tickRateHz;
    const at = arriving.at;
    const held = arriving.take();
    this.timing = tickTiming(this.tickRateHz, movement.dt);
    this.clock = new FixedStep(this.timing);
    this.renderClock = new RenderClock(this.timing.tickMs);
    this.mobs = new MobTracker(this.timing.tickMs);
    this.feed = new FeedWatch(STALL_MS);
    const view = parseMap(this.mapId, this.cache.json.get(mapKey(this.mapId)));
    this.view = view;
    this.attacks = new MobAttacks(this.timing.tickMs, view.geometry.solids);
    const style = groundStyle(view.id);
    const cam = this.cameras.main;
    cam.fadeIn(FADE_MS);

    this.background = this.add
      .image(0, 0, style.background)
      .setOrigin(0, 0)
      .setScrollFactor(0)
      .setDisplaySize(cam.width, cam.height)
      .setDepth(DEPTH.background);
    if (style.backgroundTint !== null) {
      this.background.setTint(style.backgroundTint);
    }
    this.fillBelowFloor(view, cam);
    const onResize = (size: Phaser.Structs.Size) => {
      this.background?.setDisplaySize(size.width, size.height);
      this.fillBelowFloor(view, size);
    };
    this.scale.on(Phaser.Scale.Events.RESIZE, onResize);
    this.offs.push(() => this.scale.off(Phaser.Scale.Events.RESIZE, onResize));

    for (const t of mapTiles(view)) {
      const tile = this.add
        .tileSprite(t.x, t.y, t.w, t.h, t.texture)
        .setOrigin(0, 0)
        .setTileScale(ART_SCALE)
        .setDepth(DEPTH.tiles);
      if (t.tint !== null) {
        tile.setTint(t.tint);
      }
    }

    // Hidden until the server says where the player is.
    this.player = new PlayerView(this, this.cls, this.entry.name);
    this.player.apply(
      {
        x: view.player_spawn.x,
        y: view.player_spawn.y,
        vx: 0,
        vy: 0,
        on_ground: true,
        climbing: false,
        facing: 1,
      },
      this.time.now,
    );
    this.player.setVisible(false);
    if (at) {
      this.player.apply(
        {
          x: at.x,
          y: at.y,
          vx: 0,
          vy: 0,
          on_ground: true,
          climbing: false,
          facing: 1,
        },
        this.time.now,
      );
      this.player.setVisible(true);
    }
    cam.setBounds(0, 0, view.width, view.height);
    cam.startFollow(this.player.feet, true, 0.15, 0.15); // the body, not the drawn lunge
    this.portalTags = [];
    for (const p of view.portals) {
      const glow = this.add
        .rectangle(
          p.area.x + p.area.w / 2,
          p.area.y + p.area.h / 2,
          p.area.w + 8,
          p.area.h + 8,
          0x7ac7ff,
          0.18,
        )
        .setDepth(DEPTH.glow);
      this.tweens.add({ targets: glow, alpha: 0.5, duration: 900, yoyo: true, repeat: -1 });
      const label = this.add
        .text(0, portalLabelY(p.area), portalLabel(p.target_map), {
          fontSize: '12px',
          color: '#cfe8ff',
          fontFamily: 'sans-serif',
          ...OUTLINE,
        })
        .setOrigin(0.5, 1)
        .setDepth(DEPTH.label);
      const half = label.width / 2;
      label.setX(Math.min(Math.max(p.area.x + p.area.w / 2, half + 4), view.width - half - 4));
      this.portalTags.push(tagBox(label));
    }
    for (const npc of npcsOn(view.id)) {
      this.npcViews.set(npc.id, new NpcView(this, npc));
    }
    this.showMarkers();
    // A marker follows the log, the level and, for items to bring, the inventory.
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
      }),
      inventory.subscribe((now, before) => {
        if (now.slots !== before.slots) {
          this.showMarkers();
        }
      }),
      // A potion drunk from a key or the inventory shows what it restored over the drinker only.
      combatLog.subscribe((now, before) => {
        const line = now.lines.at(-1);
        if (line?.kind === 'drank' && line !== before.lines.at(-1)) {
          this.showRestored(line.hp);
        }
      }),
    );
    this.banner(bannerLines(view.id));

    const socket = this.registry.get(REGISTRY.socket) as GameSocket;
    this.socket = socket;
    const sheet = () => character.getState().character?.sheet;
    this.predictor = new Predictor(
      view.geometry,
      this.timing,
      movementParams(view.kind, sheet()?.derived?.moveSpeed ?? 0),
      (at) => this.mobs.boxesAt(at),
    );
    this.remotes = new RemotePlayers(this.entry.characterId, this.timing.tickMs);
    this.drops.clear();
    this.numbers.clear();
    const portals = new PortalSystem(() => socket.sendAction('enterPortal'));
    this.portals = portals;
    this.numberLayer = new DamageNumberLayer(this);
    this.actions = new ActionClock(
      this.timing.tickMs,
      () => performance.now(),
      () => this.afterimage(),
    );
    // Gear, points, level-ups and a promotion change the pace; StatsChanged, LevelUp and Promoted bring the new sheet.
    this.offs.push(
      character.subscribe((now, before) => {
        if (now.character && now.character.class !== before.character?.class) {
          this.player?.setClass(now.character.class);
        }
        const next = now.character?.sheet;
        if (next === before.character?.sheet) {
          return;
        }
        this.predictor?.setParams(movementParams(view.kind, next?.derived?.moveSpeed ?? 0));
      }),
    );
    this.potionKeys = new PotionKeys({
      vitals: () => character.getState().character,
      useTab: () => inventory.getState().slots.use,
      use: (potion) =>
        socket.request('useItem', {
          idempotencyKey: idempotencyKey(),
          itemId: potion.itemId,
          template: potion.template,
        }),
      drank: (template, hp) => combatLog.getState().drank(template, hp),
      notice: (text) => toasts.getState().push(text),
    });
    this.skillKeys = new SkillKeys({
      slots: () => skillBar.getState().slots,
      caster: () => {
        const c = character.getState().character;
        return c
          ? {
              invested: c.sheet?.skills?.invested ?? {},
              energy: energy.getState().shown,
              cooldownSpeed: c.sheet?.derived?.cooldownSpeed ?? 0,
              dead: this.transitionState.dead,
              hurt: c.hp < c.maxHp,
              feet:
                this.placed && this.predictor
                  ? { x: this.predictor.state.x, y: this.predictor.state.y }
                  : null,
              hurtAllies: this.hurtAllies(c.id).map((a) => a.at),
            }
          : null;
      },
      inTown: () => this.view?.kind === 'town',
      tickMs: this.timing.tickMs,
      now: () => performance.now(),
      cooldowns: () => skillBar.getState().cooldowns,
      cool: (skill, cooldown) => skillBar.getState().cool(skill, cooldown),
      use: (skill) => socket.request('useSkill', { skill }, socket.sequence),
      notice: (text) => toasts.getState().push(text),
      free: (again) => this.actions?.press('skill', again) ?? false,
      awaitCount: (again) => this.actions?.awaitCount(again) ?? false,
      begin: (skill, stage) => this.beginSkill(skill, stage),
      spend: (cost) => energy.getState().spend(socket.sequence, cost),
    });
    const sender = new InputSender({
      send: (input, tick, at) => socket.sendInput(input, tick, at),
    });
    this.sender = sender;
    this.offs.push(
      bindKeyboard({
        target: {
          press: (a, again) => {
            if (a === 'attack') {
              this.attackHeld = true;
              if (!again) {
                this.attack(true); // a key held again after a release-all repeats as a held key does
              }
              return;
            }
            sender.press(a);
            if (a === 'up') {
              portals.keyDown(this.inPortal());
            }
          },
          release: (a) => {
            if (a === 'attack') {
              this.attackHeld = false;
              return;
            }
            sender.release(a);
            if (a === 'up') {
              portals.keyUp();
            }
          },
          releaseAll: () => {
            this.attackHeld = false;
            sender.releaseAll();
            portals.keyUp();
          },
        },
        preset: () => settings.getState().preset,
        typing: isTyping,
        onAction: (action) => {
          const key = (skillKeys as readonly Action[]).indexOf(action);
          if (key >= 0) {
            this.useSkill(key);
            return;
          }
          switch (action) {
            case 'inventory':
              ui.getState().toggleWindow('inventory');
              return;
            case 'character':
              ui.getState().toggleWindow('character');
              return;
            case 'skills':
              ui.getState().toggleWindow('skills');
              return;
            case 'party':
              ui.getState().toggleWindow('party');
              return;
            case 'quests':
              ui.getState().toggleWindow('quests');
              return;
            case 'talk':
              this.talk();
              return;
            case 'hpPotion':
              this.drink();
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
        },
      }),
      socket.on('snapshot', (m) => this.onWorld(m)),
      socket.on('delta', (m) => this.onWorld(m)),
      socket.on('welcome', (m) => this.onWelcome(m.d.mapId, m.d.tickRateHz)),
      socket.on('damage', (m) => this.onDamage(m.d, m.tick)),
      socket.on('mobAttacked', (m) =>
        this.attacks.heard(m.tick, m.d.mob, m.d.attack, m.d.facing, m.d.toX),
      ),
      socket.on('skillUsed', (m) => this.onSkillUsed(m.d)),
      socket.on('attackUsed', (m) => this.onAttackUsed(m.d)),
      socket.on('died', (m) => this.onWorld(m)),
      socket.on('respawned', (m) => this.onWorld(m)),
      socket.on('mapChange', (m) => this.onTransition(m)),
      socket.on('levelUp', () => this.onLevelUp()),
    );
    for (const m of held) {
      this.onWorld(m);
    }
  }

  private onWorld(m: Held): void {
    this.feed.heard();
    switch (m.t) {
      case 'snapshot':
        this.onSnapshot(m.d.players, m.d.mobs, m.d.drops, this.tickOf(m.tick));
        return;
      case 'delta':
        this.onDelta(
          m.d.players,
          m.d.removedPlayers,
          m.d.mobs,
          m.d.removedMobs,
          m.d.drops,
          m.d.removedDrops,
          m.d.collectedDrops,
          m.d.ackSeq,
          this.tickOf(m.tick),
        );
        return;
      case 'died':
        this.onTransition({ t: 'died', d: m.d });
        return;
      case 'respawned':
        this.onTransition({ t: 'respawned', d: m.d });
        return;
    }
  }

  /** Lets go of the socket, the keyboard and the resize handler. */
  private stopListening(): void {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
  }

  /** proto3 sends no tick as 0. */
  private tickOf(tick: number): number {
    this.lastTick = tick === 0 ? this.lastTick + 1 : tick;
    return this.lastTick;
  }

  override update(time: number, deltaMs: number): void {
    const predictor = this.predictor;
    const sender = this.sender;
    const socket = this.socket;
    if (!predictor || !sender || !socket) {
      return;
    }
    this.setStalled(this.feed.stalled(time));
    if (!this.placed) {
      return;
    }
    const dead = this.transitionState.dead;
    if (this.attackHeld) {
      this.attack(false);
    }
    for (const step of this.clock.advance(deltaMs)) {
      if (dead || this.stalled) {
        continue; // the server holds a dead player; a stalled world would take no input, so none is predicted
      }
      if (step.tickStart) {
        const at = this.renderClock.viewAt(time); // the mobs drawn this frame, which the world blocks the input against
        const input = sender.tickStart(at);
        predictor.beginTick(socket.sequence, input, at);
      }
      predictor.subStep();
    }
    if (this.player) {
      this.player.apply(predictor.state, time);
      this.player.setAlpha(this.flash.alpha(time));
    }
    this.closeDialogOutOfReach();
    this.drawRemotes(time);
    this.liftTags();
    this.drawMobs(time);
    this.landShots(this.time.now);
    this.numberLayer?.sync(this.numbers.update(time));
    skillBar.getState().endBuffs(performance.now());
  }

  private applyDrops({ added, removed, collected }: DropChanges): void {
    for (const d of added) {
      this.dropViews.set(d.id, new DropView(this, d.template, d.x, d.y));
    }
    for (const d of removed) {
      this.depart(d.id, departure(false, undefined));
    }
    for (const d of collected) {
      this.depart(d.id, departure(true, this.bodyOf(d.ownerId)));
    }
  }

  private depart(id: number, how: ReturnType<typeof departure>): void {
    const v = this.dropViews.get(id);
    this.dropViews.delete(id);
    if (how.kind === 'fly') {
      v?.flyTo(how.to);
    } else {
      v?.fade();
    }
  }

  /** Where a player on this map is drawn, at mid-body; undefined when they are not here. */
  private bodyOf(id: string): Point | undefined {
    const feet =
      id === this.entry.characterId
        ? this.placed && this.predictor
          ? { x: this.predictor.state.x, y: this.predictor.state.y }
          : undefined
        : this.remoteViews.get(id)?.feet;
    return feet && { x: feet.x, y: feet.y - BODY_MID_PX };
  }

  private drawRemotes(now: number): void {
    const remotes = this.remotes;
    const renderTime = this.renderClock.renderTime(now);
    if (!remotes || renderTime === null) {
      return;
    }
    const { shown, gone } = remotes.render(renderTime);
    for (const id of gone) {
      this.remoteViews.get(id)?.destroy();
      this.remoteViews.delete(id);
      this.forget(id);
    }
    let arrivals = false;
    for (const [id, state] of shown) {
      let v = this.remoteViews.get(id);
      if (!v) {
        v = new PlayerView(this, remotes.classOf(id), remotes.nameOf(id));
        this.remoteViews.set(id, v);
        arrivals = true;
      }
      v.setClass(remotes.classOf(id));
      v.apply(state, now);
    }
    if (arrivals || gone.length > 0) {
      nearby
        .getState()
        .setNearby([...this.remoteViews.keys()].map((id) => ({ id, name: remotes.nameOf(id) })));
    }
  }

  /** Players' tags stand clear of the portal labels, the NPCs' text and each other: the local player's first, then others in order of arrival. */
  private liftTags(): void {
    const views: PlayerView[] = [];
    const tags: TagBox[] = [];
    for (const v of [this.player, ...this.remoteViews.values()]) {
      const t = v?.nameTag();
      if (v && t) {
        views.push(v);
        tags.push(t);
      }
    }
    stackTags(this.fixedTags, tags).forEach((bottom, i) => views[i]?.liftTag(bottom));
  }

  private drawMobs(now: number): void {
    const renderTime = this.renderClock.renderTime(now);
    if (renderTime === null) {
      return;
    }
    const { shown, gone } = this.mobs.render(renderTime);
    for (const id of gone) {
      this.mobViews.get(id)?.destroy();
      this.mobViews.delete(id);
    }
    const attacked = this.attacks.frame(renderTime, shown);
    this.spits = attacked.spits;
    for (const [id, state] of shown) {
      let v = this.mobViews.get(id);
      if (!v) {
        v = new MobView(this, state.type);
        this.mobViews.set(id, v);
      }
      v.apply(state, now, attacked.poses.get(id));
    }
    this.playMobAttacks(attacked.poses, now - renderTime);
    const boss = fieldBoss.getState().boss;
    const drawn = boss && shown.get(boss.mobId);
    fieldBoss.getState().setHp(drawn ? { hp: drawn.hp, maxHp: drawn.maxHp } : null); // the boss bar shows what is drawn
  }

  /** Each mob attack's look from its windup, following the mob; it ends with the attack, a spit's once its balls are down. */
  private playMobAttacks(poses: AttacksDrawn['poses'], offset: number): void {
    for (const [id, look] of this.mobLooks) {
      const flying = look.swing.attack.kind === 'shot' && this.spits.some((s) => s.mob === id);
      if (poses.get(id)?.swing !== look.swing && !flying) {
        look.stop();
        this.mobLooks.delete(id);
      }
    }
    for (const [id, { swing }] of poses) {
      const v = this.mobViews.get(id);
      const body = this.hitBody({ kind: 'mob', id });
      if (!v || !body || this.mobLooks.get(id)?.swing === swing) {
        continue;
      }
      this.mobLooks.get(id)?.stop();
      const a = swing.attack;
      const start = swing.from + offset;
      const hit = start + a.windup_ms;
      const strikeEnd = hit + a.travel_ms;
      const readyAt = strikeEnd + IMPACT_MS;
      const flight = a.kind === 'shot' ? (a.range / a.speed) * 1000 : 0;
      const tint = mobArt(v.type).tint;
      const stop = play(this, mobEffect(a.name), {
        x: body.x,
        y: body.y,
        facing: swing.facing,
        size: { w: body.w, h: body.h },
        reach: a.kind === 'front' ? a.reach : a.spread,
        to: swing.to,
        beats: { start, hit, strikeEnd, readyAt, end: readyAt + flight, slows: false },
        follow: () => this.hitBody({ kind: 'mob', id }),
        players: () => this.playerBodies(),
        shots: () =>
          this.spits
            .filter((s) => s.mob === id)
            .map((s) => ({ x: s.x, y: s.y + s.size / 2, w: s.size, h: s.size })),
        ...(tint !== null && { color: tint }),
      });
      this.mobLooks.set(id, { swing, stop });
    }
  }

  /** Ends every mob attack's look: the mobs are gone, as on a map change. */
  private endMobAttacks(): void {
    for (const look of this.mobLooks.values()) {
      look.stop();
    }
    this.mobLooks.clear();
    this.spits = [];
  }

  private me(players: readonly Wire[]): Wire | undefined {
    return players.find((p) => p.id === this.entry.characterId);
  }

  private onSnapshot(
    players: readonly Wire[],
    mobs: readonly MobWire[],
    drops: readonly DropWire[],
    tick: number,
  ): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applySnapshot(tick * this.timing.tickMs, players);
    this.mobs.applySnapshot(tick * this.timing.tickMs, mobs);
    this.applyDrops(this.drops.applySnapshot(drops));
    const me = this.me(players);
    if (!me || !this.predictor || !this.player) {
      return;
    }
    this.predictor.reset(wireState(me));
    this.placed = true;
    this.player.setVisible(true);
    this.player.apply(this.predictor.state, this.time.now);
    this.applySelf(me);
  }

  private onDelta(
    players: readonly Wire[],
    removed: readonly string[],
    mobs: readonly MobWire[],
    removedMobs: readonly number[],
    drops: readonly DropWire[],
    removedDrops: readonly number[],
    collectedDrops: readonly number[],
    ackSeq: number,
    tick: number,
  ): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applyDelta(tick * this.timing.tickMs, players, removed);
    this.mobs.applyDelta(tick * this.timing.tickMs, mobs, removedMobs);
    this.applyDrops(this.drops.applyDelta(drops, removedDrops, collectedDrops));
    const me = this.me(players);
    if (!me || !this.predictor || !this.placed) {
      return;
    }
    if (!this.transitionState.dead) {
      this.predictor.reconcile(wireState(me), ackSeq);
    }
    this.applySelf(me);
  }

  private applySelf(me: Wire): void {
    this.pushHp(me.hp);
    this.flash.set(me.invulnerable, this.time.now);
  }

  private setStalled(stalled: boolean): void {
    if (stalled !== this.stalled) {
      this.stalled = stalled;
      connection.getState().setWorldStalled(stalled);
    }
  }

  /** After a reconnect or a rejoin the server made: the character is as the server has it, alive and on its map. The same map waits for the next Snapshot; another map, or tick rate, restarts the scene. */
  private onWelcome(mapId: string, tickRateHz: number): void {
    this.feed.heard();
    if (mapId !== this.mapId || tickRateHz !== this.tickRateHz) {
      this.restartOn(mapId, tickRateHz, false);
      return;
    }
    this.transitionState = initialTransition;
    this.player?.setDead(false);
    this.actions?.reset();
    this.placed = false;
    this.player?.setVisible(false);
    this.clearOthers();
  }

  private clearOthers(): void {
    this.remotes?.clear();
    this.mobs.clear();
    this.attacks.clear();
    this.endMobAttacks();
    this.shots.clear();
    this.landings = [];
    this.renderClock.reset();
    for (const [id, v] of this.remoteViews) {
      v.destroy();
      this.forget(id);
    }
    this.remoteViews.clear();
    nearby.getState().clear();
    for (const v of this.mobViews.values()) {
      v.destroy();
    }
    this.mobViews.clear();
    this.drops.clear();
    for (const v of this.dropViews.values()) {
      v.destroy();
    }
    this.dropViews.clear();
    this.numbers.clear();
    this.numberLayer?.sync([]);
  }

  private inPortal(): boolean {
    const p = this.predictor;
    const v = this.view;
    return (
      !!p &&
      !!v &&
      this.placed &&
      !this.stalled &&
      standsIn(p.state.x, p.state.y, v.portals) !== undefined
    );
  }

  private showMarkers(): void {
    const p = questPlayer();
    for (const [id, v] of this.npcViews) {
      const npc = npcById(id);
      v.setMarker(npc ? markerFor(npc, p) : null);
    }
    this.fixedTags = [...this.portalTags, ...[...this.npcViews.values()].flatMap((v) => v.tags())];
  }

  /** Talks to the nearest NPC within reach; with none, the key does nothing. */
  private talk(): void {
    const p = this.predictor;
    const socket = this.socket;
    if (!p || !socket || !this.placed || this.stalled || this.transitionState.dead) {
      return;
    }
    const npc = nearestInReach(this.mapId, p.state.x, p.state.y);
    if (!npc) {
      return;
    }
    socket.request('talk', { npcId: npc.id }).catch((err: unknown) => {
      if (err instanceof RequestError && err.code !== 'disconnected' && err.code !== 'timeout') {
        toasts.getState().push(err.message);
      }
    });
  }

  private useSkill(key: number): void {
    if (this.placed && !this.stalled && !this.transitionState.dead) {
      this.skillKeys?.press(key);
    }
  }

  private drink(): void {
    if (this.placed && !this.stalled && !this.transitionState.dead) {
      this.potionKeys?.press();
    }
  }

  private showRestored(hp: number): void {
    const p = this.predictor;
    if (p && hp > 0) {
      this.numbers.spawn(p.state.x, p.state.y - 90, `+${hp}`, 'healHp', this.time.now); // above the name tag
    }
  }

  /** The NPC's dialog closes once the player dies, walks out of its reach, or leaves its map. */
  private closeDialogOutOfReach(): void {
    if (!ui.getState().windows.includes('npc')) {
      return;
    }
    const dialog = quests.getState().dialog;
    const npc = dialog ? npcById(dialog.npcId) : undefined;
    const p = this.predictor;
    const gone = !npc || !p || npc.map !== this.mapId || !inReach(npc, p.state.x, p.state.y);
    if (gone || this.transitionState.dead) {
      ui.getState().closeWindow('npc');
    }
  }

  private fillBelowFloor(
    view: MapView,
    viewport: { readonly width: number; readonly height: number },
  ): void {
    this.underFloor?.destroy();
    this.underFloor = null;
    const t = belowFloor(view, viewport);
    if (!t) {
      return;
    }
    this.underFloor = this.add
      .tileSprite(t.x, t.y, t.w, t.h, t.texture)
      .setOrigin(0, 0)
      .setTileScale(ART_SCALE)
      .setDepth(DEPTH.tiles);
    if (t.tint !== null) {
      this.underFloor.setTint(t.tint);
    }
  }

  private banner(lines: readonly string[]): void {
    const cam = this.cameras.main;
    const texts = lines.map((line, i) =>
      this.add
        .text(cam.width / 2, 28 + i * 30, line, {
          fontSize: i === 0 ? '22px' : '15px',
          color: '#f4f4f8',
          fontFamily: 'sans-serif',
          stroke: '#0a0c16', // reads on a pale sky as on a dark cave
          strokeThickness: i === 0 ? 4 : 3,
        })
        .setOrigin(0.5, 0)
        .setScrollFactor(0)
        .setDepth(DEPTH.banner),
    );
    this.tweens.add({
      targets: texts,
      alpha: 0,
      delay: BANNER_MS,
      duration: FADE_MS * 2,
      onComplete: () => texts.forEach((t) => t.destroy()),
    });
  }

  /** A fresh press or a held key attacks as soon as an attack may start: at a cancelable skill's cancel point, which frees the player from its slow, or as the running action ends; a held key never goes ahead of a press waiting for its moment. */
  private attack(fresh: boolean): void {
    const clock = this.actions;
    if (this.view?.kind === 'town') {
      return; // the server runs no combat on town maps
    }
    if (this.transitionState.dead || !this.placed || this.stalled || !clock) {
      return;
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
    if (!this.socket?.sendAction('attack')) {
      return;
    }
    this.predictor?.mark(this.socket.sequence, 0); // an attack starts only at a skill's cancel point or end, so it ends any casting from the next input, as the world does
    const timing = attackTiming(
      character.getState().character?.sheet,
      this.cls,
      this.timing.tickMs,
    );
    clock.begin(timing);
    this.cutShort();
    const beats = beatsOf(timing, this.time.now, this.timing.tickMs);
    this.player?.act(beats);
    this.lastDamage.set(`p:${this.entry.characterId}`, attackOf(this.cls).damage);
    if (this.predictor) {
      const { x, y, facing } = this.predictor.state;
      this.attackLook(this.entry.characterId, this.cls, { x, y, facing, beats });
    }
  }

  /** A basic attack's look in place of the attacker's last; a ranged shot flies on whatever the attacker does next. */
  private attackLook(attacker: string, cls: string, o: PlayOptions): void {
    const a = attackOf(cls);
    if (a.kind === 'ranged') {
      this.cutShort(attacker);
      play(this, attackEffect(cls), o);
      this.aim(attacker, o.beats && { x: o.x, range: a.range, beats: o.beats });
    } else {
      this.effect(attacker, attackEffect(cls), o);
      this.aim(attacker, undefined);
    }
  }

  /** Draws another player's basic attack where they stand: its beats over its length, and its slash or shot. The player's own, already drawn, says the world counted it: it follows the Delta with its energy. */
  private onAttackUsed(d: Data<'attackUsed'>): void {
    if (d.attacker === this.entry.characterId) {
      this.actions?.counted();
      return;
    }
    const v = this.remoteViews.get(d.attacker);
    const cls = this.remotes?.classOf(d.attacker);
    if (!v || !cls) {
      return;
    }
    const beats = beatsOf(attackIn(cls, d.lengthTicks), this.time.now, this.timing.tickMs);
    v.act(beats);
    this.lastDamage.set(`p:${d.attacker}`, attackOf(cls).damage);
    const { x, y } = v.feet;
    this.attackLook(d.attacker, cls, { x, y, facing: d.facing < 0 ? -1 : 1, beats });
  }

  /** A buff that speeds the player paces the prediction as the world moves them: faster from the input after its hit, and back from the one after its end, at the speed the sheet then gives without it. */
  private buffPaces(skill: Skill, stage: Stage, timing: ActionTiming): readonly Pace[] {
    const c = character.getState().character;
    const bonus =
      skill.effect === 'buff'
        ? (bonusAt(stage.bonus, c?.sheet?.skills?.invested[skill.id] ?? 0).moveSpeed ?? 0)
        : 0;
    if (bonus <= 0 || !this.socket || !this.predictor || !this.view) {
      return [];
    }
    const fast = movementParams(
      this.view.kind,
      moveSpeedWith(c?.sheet?.derived?.moveSpeed ?? 0, bonus),
    );
    const kind = this.view.kind;
    const hit = this.socket.sequence + actionPoints(timing.ticks, stage.hit, stage.cancel).hit;
    return [
      this.predictor.pace(hit, () => fast),
      this.predictor.pace(hit + ticksIn(stage.duration_ms, this.timing.tickMs), () => {
        const now = character.getState().character;
        const sheet = {
          moveSpeed: now?.sheet?.derived?.moveSpeed ?? 0,
          skillsMoveSpeed: now?.sheet?.derivedSkills?.moveSpeed ?? 0,
        };
        const invested = now?.sheet?.skills?.invested ?? {};
        const buffs = skillBar.getState().buffs;
        return movementParams(
          kind,
          moveSpeedWithout(sheet, now?.class ?? '', invested, buffs, skill.id),
        );
      }),
    ];
  }

  /** The player's own skill starts as sent: its action, casting, beats, flash and look; the undo, for a refusal, takes them back. */
  private beginSkill(skill: Skill, stage: Stage): () => void {
    const timing = skillTiming(stage, this.timing.tickMs);
    const giveBack = this.actions?.begin(timing) ?? (() => undefined);
    const casting =
      this.socket &&
      this.predictor?.mark(this.socket.sequence, timing.ticks * this.timing.stepsPerTick);
    const paces = this.buffPaces(skill, stage, timing);
    this.cutShort();
    const beats = beatsOf(timing, this.time.now, this.timing.tickMs);
    this.player?.act(beats, colorOf(skill, stage));
    const p = this.predictor?.state;
    const me = this.entry.characterId;
    this.lastDamage.set(`p:${me}`, damageType(stage.damage));
    const tier = skill.stages.indexOf(stage);
    const reached = this.reachedBy(me, skill, stage);
    const shown =
      p &&
      this.effect(
        me,
        skillEffect(skill.id),
        { x: p.x, y: p.y, facing: p.facing, tier, beats },
        reached,
      );
    const range = shotRange(skill, stage);
    this.aim(me, p && range !== undefined ? { x: p.x, range, beats } : undefined);
    return () => {
      giveBack();
      if (casting) {
        this.predictor?.drop(casting);
      }
      for (const m of paces.toReversed()) {
        this.predictor?.dropPace(m); // the last applied first, so each gives back the pace before it
      }
      this.player?.forget(beats);
      if (shown && this.effects.get(me)?.stop === shown) {
        this.cutShort();
      }
    };
  }

  /** The player's own cancel leaves the pose it cut off behind, white and fading: the only cancel hint, and only after one. */
  private afterimage(): void {
    const p = this.player;
    const facing = this.predictor?.state.facing;
    if (p && facing) {
      play(this, 'cancel.afterimage', { x: p.feet.x, y: p.feet.y, facing, copy: p.sprite });
    }
  }

  /** Another player gone from view: their effect stops and their latest damage type goes, so a long grind keeps none of them. */
  private forget(id: string): void {
    this.cutShort(id);
    this.lastDamage.delete(`p:${id}`);
    this.shots.delete(id);
  }

  /** caster's latest action, which their next Damage comes from: a shot that flies, whose hits show as it lands, or none. */
  private aim(caster: string, shot: Shot | undefined): void {
    if (shot) {
      this.shots.set(caster, shot);
    } else {
      this.shots.delete(caster);
    }
  }

  /** A player's new action takes away the effect of the one it cuts short, the player's own by default. */
  private cutShort(id = this.entry.characterId): void {
    this.effects.get(id)?.stop();
    this.effects.delete(id);
  }

  /** Plays a player's action's effect in place of their last one still showing, toward `reached`, or else the foes it hits. */
  private effect(caster: string, id: EffectId, o: PlayOptions, reached?: readonly string[]): Stop {
    this.cutShort(caster);
    const targets = [...(reached ?? [])];
    const stop = play(this, id, {
      ...o,
      targets: () => this.bodies(targets),
      follow: () => this.hitBody({ kind: 'player', id: caster }),
    });
    const until = o.beats?.readyAt ?? -Infinity;
    this.effects.set(caster, { stop, targets, open: reached === undefined, until });
    return stop;
  }

  /** Who a skill reaches as it starts: a heal those it finds, a buff none (its aura follows its caster); a damaging one's foes come with its hits. */
  private reachedBy(caster: string, skill: Skill, stage: Stage): string[] | undefined {
    if (skill.effect === 'buff') {
      return [];
    }
    if (skill.effect === 'heal') {
      return caster === this.entry.characterId ? this.healed(stage) : [`p:${caster}`]; // another's party isn't known here
    }
    return undefined;
  }

  /** Whom the player's own heal finds, as the world picks: themselves if hurt, then the nearest hurt members it reaches. */
  private healed(stage: Stage): string[] {
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
      .map((m) => {
        const f = this.remoteViews.get(m.characterId)?.feet;
        return { id: m.characterId, at: f ? { x: f.x, y: f.y } : null };
      });
  }

  /** Every player drawn here now, this one included. */
  private playerBodies(): Body[] {
    return [this.entry.characterId, ...this.remoteViews.keys()].flatMap((id) => {
      const b = this.hitBody({ kind: 'player', id });
      return b ? [b] : [];
    });
  }

  /** The bodies of refs drawn here now. */
  private bodies(refs: readonly string[]): Body[] {
    return refs.flatMap((r) => {
      const ref = parseRef(r);
      const b = ref && this.hitBody(ref);
      return b ? [b] : [];
    });
  }

  /** Another player's skill, drawn from its arrival in place of their last action; the player's own buff is timed here for the character window. */
  private onSkillUsed(d: Data<'skillUsed'>): void {
    const skill = skillById(d.skill);
    const stage = skill?.stages[d.stage];
    if (!skill || !stage) {
      return;
    }
    if (d.caster !== this.entry.characterId) {
      const v = this.remoteViews.get(d.caster);
      if (v) {
        const timing = skillTiming(stage, this.timing.tickMs);
        const beats = beatsOf(timing, this.time.now, this.timing.tickMs);
        v.act(beats, colorOf(skill, stage));
        this.lastDamage.set(`p:${d.caster}`, damageType(stage.damage));
        const { x, y } = v.feet;
        const facing = d.facing < 0 ? -1 : 1;
        const o = { x, y, facing, tier: d.stage, beats } as const;
        this.effect(d.caster, skillEffect(skill.id), o, this.reachedBy(d.caster, skill, stage));
        const range = shotRange(skill, stage);
        this.aim(d.caster, range === undefined ? undefined : { x, range, beats });
      }
      return;
    }
    if (skill.effect === 'buff') {
      const points = character.getState().character?.sheet?.skills?.invested[d.skill] ?? 0;
      skillBar
        .getState()
        .buff({ skill: d.skill, points, until: performance.now() + stage.duration_ms });
    }
  }

  /** A hit, drawn only (HP comes from the server's state): the player's own holds pose and mob a moment; each sparks and pops a number. */
  private onDamage(d: Data<'damage'>, tick: number): void {
    const from = parseRef(d.source);
    if (from?.kind === 'mob') {
      this.attacks.struckBy(tick, from.id); // its spit ends on the one it hit
    }
    const target = parseRef(d.target);
    if (!target) {
      return;
    }
    const now = this.time.now;
    const body = this.hitBody(target);
    const showing = from?.kind === 'player' ? this.effects.get(from.id) : undefined;
    if (
      body &&
      showing?.open &&
      !d.miss &&
      now < showing.until &&
      !showing.targets.includes(d.target)
    ) {
      showing.targets.push(d.target); // a foe the action reached, for Arc and Volley to go to
    }
    const shot = from?.kind === 'player' ? this.shots.get(from.id) : undefined;
    const at = shot && body ? arrival(shot, body.x) : now; // a drawn shot shows its hit where and when it visibly lands
    const hit = { d, target, damage: this.damageFrom(d.source), at, last: body };
    if (at > now) {
      this.landings.push({ ...hit, ...(shot && { beats: shot.beats }) });
      return;
    }
    this.land(hit, now);
  }

  /** Ranged hits whose drawn shot has reached its target show now, where it is drawn or was last. */
  private landShots(now: number): void {
    this.landings = this.landings.filter((l) => {
      l.last = this.hitBody(l.target) ?? l.last;
      if (now < l.at) {
        return true;
      }
      this.land(l, now);
      return false;
    });
  }

  /** A hit shown: the player's own holds their pose and each mob it struck; each sparks and pops a number at its target, drawn or last seen. */
  private land(l: Landing, now: number): void {
    const { d, target } = l;
    const held =
      d.source === `p:${this.entry.characterId}` && !d.miss && this.player?.hitStop(now, l.beats);
    if (held && target.kind === 'mob') {
      this.mobViews.get(target.id)?.hold(held.until);
    }
    const body = this.hitBody(target) ?? l.last;
    if (!body) {
      return;
    }
    const from = parseRef(d.source);
    const source = from && this.hitBody(from);
    const facing = source && source.x > body.x ? -1 : 1;
    const size = { w: body.w, h: body.h };
    for (const id of hitEffects(l.damage, d)) {
      play(this, id, { x: body.x, y: body.y - body.h / 2, facing, size });
    }
    const isMe = target.kind === 'player' && target.id === this.entry.characterId;
    const n = numberFor(d, isMe);
    const above = target.kind === 'mob' ? 44 : 72;
    this.numbers.spawn(body.x, body.y - above, n.text, n.kind, now);
  }

  /** A player's or mob's box as drawn now, its feet at (x, y); undefined when it isn't drawn here. */
  private hitBody(r: Ref): { x: number; y: number; w: number; h: number } | undefined {
    if (r.kind === 'mob') {
      const v = this.mobViews.get(r.id);
      const m = v && mobs.find((t) => t.id === v.type);
      return v && { x: v.feet.x, y: v.feet.y, w: m?.width ?? 32, h: m?.height ?? 32 };
    }
    const feet =
      r.id === this.entry.characterId ? this.predictor?.state : this.remoteViews.get(r.id)?.feet;
    return feet && { x: feet.x, y: feet.y, w: movement.width, h: movement.height };
  }

  /** The damage type of a hit from source: a mob type's own, else the player's latest action's, else physical. */
  private damageFrom(source: string): Damage {
    const r = parseRef(source);
    if (r?.kind === 'mob') {
      const type = this.mobViews.get(r.id)?.type;
      return damageType(mobs.find((m) => m.id === type)?.damage ?? null);
    }
    return this.lastDamage.get(source) ?? 'physical';
  }

  private onTransition(msg: TransitionMsg): void {
    const r = transition(this.transitionState, msg, this.entry.characterId, this.mapId);
    this.transitionState = r.state;
    for (const a of r.actions) {
      this.applyTransition(a);
    }
  }

  private applyTransition(a: TransitionAction): void {
    switch (a.type) {
      case 'die':
        this.player?.setDead(true);
        this.flash.set(false, this.time.now);
        this.actions?.reset();
        skillBar.getState().endBuffs(); // the world ends them at death
        character.getState().setDead(true);
        ui.getState().closeWindow('npc');
        return;
      case 'respawn-here':
        this.player?.setDead(false);
        this.predictor?.reset({
          x: a.x,
          y: a.y,
          vx: 0,
          vy: 0,
          on_ground: true,
          climbing: false,
          facing: 1,
          casting: 0,
        });
        this.pushHp(a.hp);
        character.getState().setDead(false);
        return;
      case 'change-map':
        character.getState().setDead(false);
        this.portals?.arrived();
        this.restartOn(a.mapId, this.tickRateHz, true, { x: a.x, y: a.y });
        return;
    }
  }

  /** From now on an Arrival holds the next map's messages, its Snapshot too, and this scene applies none; the restart builds from it, after the fade if any. */
  private restartOn(mapId: string, tickRateHz: number, fade: boolean, at?: Spot): void {
    const socket = this.socket;
    if (!socket) {
      return;
    }
    const next = new Arrival(socket, mapId, tickRateHz, at);
    this.stopListening();
    this.placed = false;
    const restart = () => {
      this.arriving = null;
      this.scene.restart({ ...this.entry, arriving: next });
    };
    if (!fade) {
      restart();
      return;
    }
    this.arriving = next; // a scene stopped during the fade lets go of it in teardown
    this.cameras.main.fadeOut(FADE_MS);
    this.cameras.main.once(Phaser.Cameras.Scene2D.Events.FADE_OUT_COMPLETE, restart);
  }

  private onLevelUp(): void {
    if (this.predictor && this.placed) {
      this.numbers.spawn(
        this.predictor.state.x,
        this.predictor.state.y - 80,
        'LEVEL UP!',
        'crit',
        this.time.now,
      );
    }
  }

  private pushHp(hp: number): void {
    character.getState().setHp(hp);
  }

  private teardown(): void {
    this.stopListening();
    this.arriving?.take(); // stopped before it was built, or during the fade
    this.arriving = null;
    this.clearOthers();
    for (const v of this.npcViews.values()) {
      v.destroy();
    }
    this.npcViews.clear();
    this.portalTags = [];
    this.fixedTags = [];
    this.numberLayer?.destroy();
    this.numberLayer = null;
    this.remotes = null;
    this.player = null;
    this.background = null;
    this.underFloor = null;
    this.socket = null;
    this.predictor = null;
    this.sender = null;
    this.actions?.reset();
    this.actions = null;
    this.effects.clear();
    this.lastDamage.clear();
    this.endMobAttacks();
    this.shots.clear();
    this.landings = [];
    this.potionKeys = null;
    this.skillKeys = null;
    this.attackHeld = false;
    this.portals = null;
    this.view = null;
    this.placed = false;
    this.setStalled(false);
    this.transitionState = initialTransition;
  }
}
