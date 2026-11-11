import Phaser from 'phaser';

import { movement } from '@/gamedata/movement';
import { isTyping } from '@/input/typing';
import { FixedStep, tickTiming } from '@/lib/fixed-step';
import type { TickTiming } from '@/lib/fixed-step';
import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import type { Vital } from '@/progression/potions';
import { inReach, markerFor, nearestInReach, npcById, npcsOn } from '@/progression/quests';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import type { MapView } from '@/sim/map';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { connection } from '@/state/connection';
import { inventory } from '@/state/inventory';
import { nearby } from '@/state/nearby';
import { questPlayer, quests } from '@/state/quests';
import { settings } from '@/state/settings';
import { toasts } from '@/state/toasts';
import { ui } from '@/state/ui';

import { DamageNumberLayer } from '../entities/damage-number';
import { DropView } from '../entities/drop';
import { MobView } from '../entities/mob';
import { NpcView } from '../entities/npc';
import { PlayerView } from '../entities/player';
import { ART_SCALE, SCENES, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import { Arrival } from '../systems/arrival';
import type { Held, Spot } from '../systems/arrival';
import {
  AttackGate,
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  attackCooldownTicks,
  numberFor,
  parseRef,
} from '../systems/combat-view';
import { DropTracker, departure } from '../systems/drops';
import type { DropChanges, Point } from '../systems/drops';
import { FeedWatch } from '../systems/feed-watch';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import { PortalSystem, standsIn } from '../systems/portals';
import { PotionKeys } from '../systems/potion-keys';
import { Predictor, movementParams, wireState } from '../systems/prediction';

import { bannerLines, groundStyle, mapTiles } from './world-layout';

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
const ARROW_RANGE = 420;
const ARROW_MS = 300;

export class World extends Phaser.Scene {
  private entry: Omit<WorldData, 'arriving'> = {
    characterId: '',
    cls: 'warrior',
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
  private readonly mobViews = new Map<number, MobView>();
  private readonly numbers = new DamageNumbers();
  private numberLayer: DamageNumberLayer | null = null;
  private attackGate: AttackGate | null = null;
  /** The attack key is down: the gate lets a strike through at each cooldown. */
  private attackHeld = false;
  private potionKeys: PotionKeys | null = null;
  private readonly flash = new InvulnFlash();
  private transitionState: TransitionState = initialTransition;

  private readonly drops = new DropTracker();
  private readonly dropViews = new Map<number, DropView>();

  private view: MapView | null = null;
  private portals: PortalSystem | null = null;
  private readonly npcViews = new Map<string, NpcView>();

  constructor() {
    super(SCENES.World);
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
    const style = groundStyle(view.id);
    const cam = this.cameras.main;
    cam.fadeIn(FADE_MS);

    this.background = this.add
      .image(0, 0, style.background)
      .setOrigin(0, 0)
      .setScrollFactor(0)
      .setDisplaySize(cam.width, cam.height)
      .setDepth(-2);
    const onResize = (size: Phaser.Structs.Size) => {
      this.background?.setDisplaySize(size.width, size.height);
    };
    this.scale.on(Phaser.Scale.Events.RESIZE, onResize);
    this.offs.push(() => this.scale.off(Phaser.Scale.Events.RESIZE, onResize));

    for (const t of mapTiles(view)) {
      this.add.tileSprite(t.x, t.y, t.w, t.h, t.texture).setOrigin(0, 0).setTileScale(ART_SCALE);
    }

    // Hidden until the server says where the player is.
    this.player = new PlayerView(this, this.entry.cls, this.entry.name);
    this.player.apply({
      x: view.player_spawn.x,
      y: view.player_spawn.y,
      vx: 0,
      vy: 0,
      on_ground: true,
      climbing: false,
      facing: 1,
    });
    this.player.setVisible(false);
    if (at) {
      this.player.apply({
        x: at.x,
        y: at.y,
        vx: 0,
        vy: 0,
        on_ground: true,
        climbing: false,
        facing: 1,
      });
      this.player.setVisible(true);
    }
    cam.setBounds(0, 0, view.width, view.height);
    cam.startFollow(this.player.sprite, true, 0.15, 0.15);
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
        .setDepth(-1);
      this.tweens.add({ targets: glow, alpha: 0.5, duration: 900, yoyo: true, repeat: -1 });
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
          this.showRestored(line.hp, line.mp);
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
    );
    this.remotes = new RemotePlayers(this.entry.characterId, this.timing.tickMs);
    this.drops.clear();
    this.numbers.clear();
    const portals = new PortalSystem(() => socket.sendAction('enterPortal'));
    this.portals = portals;
    this.numberLayer = new DamageNumberLayer(this);
    this.attackGate = new AttackGate(
      attackCooldownTicks(sheet(), this.entry.cls, this.timing.tickMs),
      this.timing.tickMs,
      () => socket.sendAction('attack'),
    );
    // Gear, points and level-ups change the pace; StatsChanged and LevelUp bring the new sheet.
    this.offs.push(
      character.subscribe((now, before) => {
        const next = now.character?.sheet;
        if (next === before.character?.sheet) {
          return;
        }
        this.attackGate?.setCooldownTicks(
          attackCooldownTicks(next, this.entry.cls, this.timing.tickMs),
        );
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
      drank: (template, restored) => combatLog.getState().drank(template, restored),
      notice: (text) => toasts.getState().push(text),
    });
    const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
    this.sender = sender;
    this.offs.push(
      bindKeyboard({
        target: {
          press: (a) => {
            if (a === 'attack') {
              this.attackHeld = true;
              this.attack();
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
          switch (action) {
            case 'inventory':
              ui.getState().toggleWindow('inventory');
              return;
            case 'character':
              ui.getState().toggleWindow('character');
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
              this.drink('hp');
              return;
            case 'mpPotion':
              this.drink('mp');
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
      socket.on('correction', (m) => this.onCorrection(m.d)),
      socket.on('welcome', (m) => this.onWelcome(m.d.mapId, m.d.tickRateHz)),
      socket.on('damage', (m) => this.onDamage(m.d)),
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
      this.attack();
    }
    for (const step of this.clock.advance(deltaMs)) {
      if (dead || this.stalled) {
        continue; // the server holds a dead player; a stalled world would take no input, so none is predicted
      }
      if (step.tickStart) {
        const input = sender.tickStart();
        predictor.beginTick(socket.sequence, input);
      }
      predictor.subStep();
    }
    if (this.player) {
      this.player.apply(predictor.state, time);
      this.player.setAlpha(this.flash.alpha(time));
    }
    this.closeDialogOutOfReach();
    this.drawRemotes(time);
    this.drawMobs(time);
    this.numberLayer?.sync(this.numbers.update(time));
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
        : this.remoteViews.get(id)?.sprite;
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
    }
    let arrivals = false;
    for (const [id, state] of shown) {
      let v = this.remoteViews.get(id);
      if (!v) {
        v = new PlayerView(this, remotes.classOf(id), remotes.nameOf(id));
        this.remoteViews.set(id, v);
        arrivals = true;
      }
      v.apply(state);
    }
    if (arrivals || gone.length > 0) {
      nearby
        .getState()
        .setNearby([...this.remoteViews.keys()].map((id) => ({ id, name: remotes.nameOf(id) })));
    }
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
    for (const [id, state] of shown) {
      let v = this.mobViews.get(id);
      if (!v) {
        v = new MobView(this, state.type);
        this.mobViews.set(id, v);
      }
      v.apply(state);
    }
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
    this.player.apply(this.predictor.state);
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

  /** A Correction carries no facing; the current one stays. */
  private onCorrection(c: Data<'correction'>): void {
    if (this.predictor && this.placed) {
      this.predictor.correct(wireState({ ...c, facing: this.predictor.state.facing }), c.seq);
    }
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
    this.attackGate?.reset();
    this.placed = false;
    this.player?.setVisible(false);
    this.clearOthers();
  }

  private clearOthers(): void {
    this.remotes?.clear();
    this.mobs.clear();
    this.renderClock.reset();
    for (const v of this.remoteViews.values()) {
      v.destroy();
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

  private drink(vital: Vital): void {
    if (this.placed && !this.stalled && !this.transitionState.dead) {
      this.potionKeys?.press(vital);
    }
  }

  private showRestored(hp: number, mp: number): void {
    const p = this.predictor;
    if (!p) {
      return;
    }
    const now = this.time.now;
    let y = p.state.y - 72;
    if (hp > 0) {
      this.numbers.spawn(p.state.x, y, `+${hp}`, 'healHp', now);
      y -= 18;
    }
    if (mp > 0) {
      this.numbers.spawn(p.state.x, y, `+${mp}`, 'healMp', now);
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

  private banner(lines: readonly string[]): void {
    const cam = this.cameras.main;
    const texts = lines.map((line, i) =>
      this.add
        .text(cam.width / 2, 28 + i * 30, line, {
          fontSize: i === 0 ? '22px' : '15px',
          color: '#f4f4f8',
          fontFamily: 'sans-serif',
        })
        .setOrigin(0.5, 0)
        .setScrollFactor(0)
        .setDepth(20),
    );
    this.tweens.add({
      targets: texts,
      alpha: 0,
      delay: BANNER_MS,
      duration: FADE_MS * 2,
      onComplete: () => texts.forEach((t) => t.destroy()),
    });
  }

  private attack(): void {
    const now = this.time.now;
    if (this.view?.kind === 'town') {
      return; // the server runs no combat on town maps
    }
    if (
      this.transitionState.dead ||
      !this.placed ||
      this.stalled ||
      !this.attackGate?.tryAttack(now)
    ) {
      return;
    }
    this.player?.attack(now);
    if (this.entry.cls === 'bowman' && this.predictor) {
      this.arrow(this.predictor.state.x, this.predictor.state.y - 28, this.predictor.state.facing);
    }
  }

  private arrow(x: number, y: number, facing: 1 | -1): void {
    const streak = this.add.rectangle(x, y, 16, 3, 0xf0ead6).setDepth(5);
    this.tweens.add({
      targets: streak,
      x: x + facing * ARROW_RANGE,
      duration: ARROW_MS,
      onComplete: () => streak.destroy(),
    });
  }

  /** Only the number: HP comes from the server's state alone, so a hit already in the Delta is not taken twice. */
  private onDamage(d: Data<'damage'>): void {
    const target = parseRef(d.target);
    if (!target) {
      return;
    }
    const now = this.time.now;
    if (target.kind === 'mob') {
      const v = this.mobViews.get(target.id);
      if (v) {
        const n = numberFor(d, false);
        this.numbers.spawn(v.sprite.x, v.sprite.y - 44, n.text, n.kind, now);
      }
      return;
    }
    const isMe = target.id === this.entry.characterId;
    const at = isMe
      ? this.predictor && { x: this.predictor.state.x, y: this.predictor.state.y }
      : this.remoteViews.get(target.id)?.sprite;
    if (at) {
      const n = numberFor(d, isMe);
      this.numbers.spawn(at.x, at.y - 72, n.text, n.kind, now);
    }
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
        this.attackGate?.reset();
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

  /** MP is not on the wire, so only HP is pushed. */
  private pushHp(hp: number): void {
    const c = character.getState().character;
    if (c && c.hp !== hp) {
      character.getState().setVitals(hp, c.mp);
    }
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
    this.numberLayer?.destroy();
    this.numberLayer = null;
    this.remotes = null;
    this.player = null;
    this.background = null;
    this.socket = null;
    this.predictor = null;
    this.sender = null;
    this.attackGate = null;
    this.potionKeys = null;
    this.attackHeld = false;
    this.portals = null;
    this.view = null;
    this.placed = false;
    this.setStalled(false);
    this.transitionState = initialTransition;
  }
}
