// World: draws the map named in Welcome from its object layers, the local
// player predicted one tick ahead of the server (C08), the other players
// and the mobs interpolated 100 ms behind it (C09, C10), damage numbers,
// death and respawn, all from the server's messages. A Welcome after a
// reconnect reloads the map when it changed and waits for the next
// Snapshot. The scene stays thin: it calls the systems.
import Phaser from 'phaser';

import { FixedStep } from '@/lib/fixed-step';
import type { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import type { MapView } from '@/sim/map';
import { character } from '@/state/character';
import { settings } from '@/state/settings';
import { ui } from '@/state/ui';

import { DamageNumberLayer } from '../entities/damage-number';
import { DropView } from '../entities/drop';
import { MobView } from '../entities/mob';
import { PlayerView } from '../entities/player';
import { ART_SCALE, SCENES, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import {
  AttackGate,
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  classCooldownMs,
  numberFor,
  parseRef,
} from '../systems/combat-view';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock, TICK_MS } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { initialTransition, transition } from '../systems/map-transition';
import type { TransitionAction, TransitionMsg, TransitionState } from '../systems/map-transition';
import { DropTracker, PickupSystem, boxCentre } from '../systems/pickup';
import { PortalSystem, standsIn } from '../systems/portals';
import { Predictor } from '../systems/prediction';

import { groundStyle, mapTiles } from './world-layout';

/** What the app passes when the player enters the world, from Welcome. */
export interface WorldData {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
  /** After a MapChange: where the player arrives, shown until the Snapshot confirms it. */
  readonly arrival?: { readonly x: number; readonly y: number };
}

const FADE_MS = 250;
const BANNER_MS = 1800;

/** "forest" as "Forest" for the arrival banner. */
function mapTitle(id: string): string {
  return id.charAt(0).toUpperCase() + id.slice(1).replace(/_/g, ' ');
}

type Wire = Data<'Snapshot'>['players'][number];
type MobWire = Data<'Snapshot'>['mobs'][number];
type DropWire = Data<'Snapshot'>['drops'][number];

const ARROW_RANGE = 420;
const ARROW_MS = 300;

export class World extends Phaser.Scene {
  private entry: WorldData = { mapId: 'town', characterId: '', cls: 'warrior', name: '' };
  private player: PlayerView | null = null;
  private background: Phaser.GameObjects.Image | null = null;
  private offs: (() => void)[] = [];

  private socket: GameSocket | null = null;
  private predictor: Predictor | null = null;
  private sender: InputSender | null = null;
  private readonly clock = new FixedStep();
  private placed = false; // the first Snapshot has named the local player

  private remotes: RemotePlayers | null = null;
  private readonly remoteViews = new Map<string, PlayerView>();
  private readonly renderClock = new RenderClock();
  private lastTick = 0;

  private readonly mobs = new MobTracker();
  private readonly mobViews = new Map<number, MobView>();
  private readonly numbers = new DamageNumbers();
  private numberLayer: DamageNumberLayer | null = null;
  private attackGate: AttackGate | null = null;
  private readonly flash = new InvulnFlash();
  private transitionState: TransitionState = initialTransition;

  private readonly drops = new DropTracker();
  private readonly dropViews = new Map<number, DropView>();
  private pickup: PickupSystem | null = null;
  private hinted: number | null = null; // the drop within reach, if any

  private view: MapView | null = null;
  private portals: PortalSystem | null = null;

  constructor() {
    super(SCENES.World);
  }

  init(data: WorldData): void {
    this.entry = data;
    this.placed = false;
    this.transitionState = initialTransition;
  }

  create(): void {
    this.events.once(Phaser.Scenes.Events.SHUTDOWN, () => this.teardown());
    if (this.registry.get(REGISTRY.assetsReady) === true) {
      this.build();
      return;
    }
    const onReady = () => this.build();
    this.game.events.once(EVENTS.assetsReady, onReady);
    this.offs.push(() => this.game.events.off(EVENTS.assetsReady, onReady));
  }

  private build(): void {
    const view = parseMap(this.entry.mapId, this.cache.json.get(mapKey(this.entry.mapId)));
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

    // Standing at the map's entry, hidden until the server says where it is.
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
    if (this.entry.arrival) {
      // Through a portal: stand where MapChange said until the Snapshot confirms it.
      this.player.apply({
        x: this.entry.arrival.x,
        y: this.entry.arrival.y,
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
      // A soft glow over each portal, breathing.
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
    this.banner(mapTitle(view.id));

    const socket = this.registry.get(REGISTRY.socket) as GameSocket;
    this.socket = socket;
    this.predictor = new Predictor(view.geometry);
    this.remotes = new RemotePlayers(this.entry.characterId);
    this.renderClock.reset();
    this.mobs.clear();
    this.drops.clear();
    this.numbers.clear();
    this.pickup = new PickupSystem(() => socket.sendAction('PickUp'));
    const portals = new PortalSystem(() => socket.sendAction('EnterPortal'));
    this.portals = portals;
    this.numberLayer = new DamageNumberLayer(this);
    this.attackGate = new AttackGate(classCooldownMs(this.entry.cls), () =>
      socket.sendAction('Attack'),
    );
    const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
    this.sender = sender;
    sender.setCaptured(ui.getState().captureKeyboard);
    this.offs.push(
      bindKeyboard({
        // Movement keys go to the input sender; the up key also asks the portals.
        target: {
          press: (a) => {
            sender.press(a);
            if (a === 'up') {
              portals.keyDown(this.inPortal());
            }
          },
          release: (a) => {
            sender.release(a);
            if (a === 'up') {
              portals.keyUp();
            }
          },
          releaseAll: () => {
            sender.releaseAll();
            portals.keyUp();
          },
        },
        preset: () => settings.getState().preset,
        captured: () => ui.getState().captureKeyboard,
        onAction: (action) => {
          switch (action) {
            case 'attack':
              this.attack();
              return;
            case 'pickup':
              this.pickUp();
              return;
            case 'inventory':
              ui.getState().toggleWindow('inventory');
              return;
            default:
              return; // party and chat come with C13
          }
        },
      }),
      ui.subscribe((s) => sender.setCaptured(s.captureKeyboard)),
      socket.on('Snapshot', (m) =>
        this.onSnapshot(m.d.players, m.d.mobs, m.d.drops, this.tickOf(m.tick)),
      ),
      socket.on('Delta', (m) =>
        this.onDelta(
          m.d.players,
          m.d.removed_players,
          m.d.mobs,
          m.d.removed_mobs,
          m.d.drops,
          m.d.removed_drops,
          m.d.ack_seq,
          this.tickOf(m.tick),
        ),
      ),
      socket.on('Correction', (m) => this.onCorrection(m.d)),
      socket.on('Welcome', (m) => this.onWelcome(m.d.map_id)),
      socket.on('Damage', (m) => this.onDamage(m.d)),
      socket.on('Died', (m) => this.onTransition({ t: 'Died', d: m.d })),
      socket.on('Respawned', (m) => this.onTransition({ t: 'Respawned', d: m.d })),
      socket.on('MapChange', (m) => this.onTransition({ t: 'MapChange', d: m.d })),
      socket.on('LevelUp', () => this.onLevelUp()),
    );
  }

  /** The envelope's tick, or the next one when a message carries none. */
  private tickOf(tick: number | undefined): number {
    this.lastTick = tick ?? this.lastTick + 1;
    return this.lastTick;
  }

  /** The frame loop: whole sub-steps only; a tick boundary samples and sends the keys. */
  override update(time: number, deltaMs: number): void {
    const predictor = this.predictor;
    const sender = this.sender;
    const socket = this.socket;
    if (!predictor || !sender || !socket || !this.placed) {
      return;
    }
    const dead = this.transitionState.dead;
    for (const step of this.clock.advance(deltaMs)) {
      if (dead) {
        continue; // the server holds a dead player; nothing is sent or predicted
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
    this.drawRemotes(time);
    this.drawMobs(time);
    this.hintDrops();
    this.numberLayer?.sync(this.numbers.update(time));
  }

  /** The drop within reach, if any, is marked; the pickup key acts on it. */
  private hintDrops(): void {
    const p = this.predictor;
    if (!p) {
      return;
    }
    const c = boxCentre(p.state.x, p.state.y);
    const near = this.drops.nearest(c.x, c.y)?.id ?? null;
    if (near === this.hinted) {
      return;
    }
    if (this.hinted !== null) {
      this.dropViews.get(this.hinted)?.setHint(false);
    }
    if (near !== null) {
      this.dropViews.get(near)?.setHint(true);
    }
    this.hinted = near;
  }

  private applyDrops(added: readonly DropWire[], removed: readonly number[]): void {
    for (const id of removed) {
      this.dropViews.get(id)?.destroy();
      this.dropViews.delete(id);
      if (this.hinted === id) {
        this.hinted = null;
      }
    }
    for (const d of added) {
      this.dropViews.set(d.id, new DropView(this, d.template, d.x, d.y));
    }
  }

  /** The pickup key: one PickUp per tick, only with a drop in reach; the inventory follows InventoryChanged. */
  private pickUp(): void {
    if (this.transitionState.dead || !this.placed || !this.pickup || !this.sender) {
      return;
    }
    this.pickup.press(this.hinted !== null, this.sender.currentTick);
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
    for (const [id, state] of shown) {
      let v = this.remoteViews.get(id);
      if (!v) {
        v = new PlayerView(this, remotes.classOf(id), remotes.nameOf(id));
        this.remoteViews.set(id, v);
      }
      v.apply(state);
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

  /** A Snapshot is the truth: it places the local player and replaces the others and the mobs. */
  private onSnapshot(
    players: readonly Wire[],
    mobs: readonly MobWire[],
    drops: readonly DropWire[],
    tick: number,
  ): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applySnapshot(tick * TICK_MS, players);
    this.mobs.applySnapshot(tick * TICK_MS, mobs);
    const changed = this.drops.applySnapshot(drops);
    this.applyDrops(changed.added, changed.removed);
    const me = this.me(players);
    if (!me || !this.predictor || !this.player) {
      return;
    }
    this.predictor.reset(me);
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
    ackSeq: number,
    tick: number,
  ): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applyDelta(tick * TICK_MS, players, removed);
    this.mobs.applyDelta(tick * TICK_MS, mobs, removedMobs);
    const changed = this.drops.applyDelta(drops, removedDrops);
    this.applyDrops(changed.added, changed.removed);
    const me = this.me(players);
    if (!me || !this.predictor || !this.placed) {
      return;
    }
    if (!this.transitionState.dead) {
      this.predictor.reconcile(me, ackSeq);
    }
    this.applySelf(me);
  }

  /** The server's word on the local player's HP and flags. */
  private applySelf(me: Wire): void {
    this.pushHp(me.hp);
    this.flash.set(me.invulnerable, this.time.now);
  }

  /** A Correction (reserved on page 4) has no facing; the current one stays. */
  private onCorrection(c: Data<'Correction'>): void {
    if (this.predictor && this.placed) {
      this.predictor.correct({ ...c, facing: this.predictor.state.facing }, c.seq);
    }
  }

  /**
   * A Welcome after a reconnect: the same map waits for the next Snapshot
   * to place everyone again; another map (the player moved while away, or
   * respawned in town) reloads the scene for it.
   */
  private onWelcome(mapId: string): void {
    if (mapId !== this.entry.mapId) {
      this.scene.restart({ ...this.entry, mapId });
      return;
    }
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
    for (const v of this.mobViews.values()) {
      v.destroy();
    }
    this.mobViews.clear();
    this.drops.clear();
    for (const v of this.dropViews.values()) {
      v.destroy();
    }
    this.dropViews.clear();
    this.hinted = null;
    this.numbers.clear();
    this.numberLayer?.sync([]);
  }

  /** The attack key: the class cooldown gates sending; the server decides hits. */
  /** Whether the predicted feet stand in a portal right now (edges included, page 5). */
  private inPortal(): boolean {
    const p = this.predictor;
    const v = this.view;
    return !!p && !!v && this.placed && standsIn(p.state.x, p.state.y, v.portals) !== undefined;
  }

  /** The map's name for a moment on arrival. */
  private banner(text: string): void {
    const cam = this.cameras.main;
    const t = this.add
      .text(cam.width / 2, 28, text, {
        fontSize: '22px',
        color: '#f4f4f8',
        fontFamily: 'sans-serif',
      })
      .setOrigin(0.5, 0)
      .setScrollFactor(0)
      .setDepth(20);
    this.tweens.add({
      targets: t,
      alpha: 0,
      delay: BANNER_MS,
      duration: FADE_MS * 2,
      onComplete: () => t.destroy(),
    });
  }

  private attack(): void {
    const now = this.time.now;
    if (this.view?.kind === 'town') {
      return; // the server runs no combat on town maps
    }
    if (this.transitionState.dead || !this.placed || !this.attackGate?.tryAttack(now)) {
      return;
    }
    this.player?.attack(now);
    if (this.entry.cls === 'bowman' && this.predictor) {
      this.arrow(this.predictor.state.x, this.predictor.state.y - 28, this.predictor.state.facing);
    }
  }

  /** The Bowman's arrow: a streak flying the attack range in the facing direction. */
  private arrow(x: number, y: number, facing: 1 | -1): void {
    const streak = this.add.rectangle(x, y, 16, 3, 0xf0ead6).setDepth(5);
    this.tweens.add({
      targets: streak,
      x: x + facing * ARROW_RANGE,
      duration: ARROW_MS,
      onComplete: () => streak.destroy(),
    });
  }

  /** A resolved hit: a number over the target, the mob's bar, the local player's HP. */
  private onDamage(d: Data<'Damage'>): void {
    const target = parseRef(d.target);
    if (!target) {
      return;
    }
    const now = this.time.now;
    if (target.kind === 'mob') {
      if (!d.miss) {
        this.mobs.damage(target.id, d.amount);
      }
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
    if (isMe && !d.miss) {
      const c = character.getState().character;
      if (c) {
        this.pushHp(Math.max(0, c.hp - d.amount)); // the next Delta confirms
      }
    }
  }

  /** Died, Respawned and MapChange for the local player, through the transition machine. */
  private onTransition(msg: TransitionMsg): void {
    const r = transition(this.transitionState, msg, this.entry.characterId, this.entry.mapId);
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
      case 'change-map': {
        character.getState().setDead(false);
        this.portals?.arrived();
        // Fade out, then the scene restarts for the named map; its Snapshot follows.
        const next: WorldData = { ...this.entry, mapId: a.mapId, arrival: { x: a.x, y: a.y } };
        this.placed = false;
        this.cameras.main.fadeOut(FADE_MS);
        this.cameras.main.once(Phaser.Cameras.Scene2D.Events.FADE_OUT_COMPLETE, () => {
          this.scene.restart(next);
        });
        return;
      }
    }
  }

  /** A level-up effect on the entity; the HUD follows the store (wiring). */
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

  /** HP into the character store only when it changed (the store checks); MP is not on the wire. */
  private pushHp(hp: number): void {
    const c = character.getState().character;
    if (c && c.hp !== hp) {
      character.getState().setVitals(hp, c.mp);
    }
  }

  private teardown(): void {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
    this.clearOthers();
    this.numberLayer?.destroy();
    this.numberLayer = null;
    this.remotes = null;
    this.player = null;
    this.background = null;
    this.socket = null;
    this.predictor = null;
    this.sender = null;
    this.attackGate = null;
    this.pickup = null;
    this.portals = null;
    this.view = null;
    this.placed = false;
    this.transitionState = initialTransition;
  }
}
