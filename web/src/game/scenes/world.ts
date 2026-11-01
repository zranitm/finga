import Phaser from 'phaser';

import { FixedStep } from '@/lib/fixed-step';
import type { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import type { MapView } from '@/sim/map';
import { character } from '@/state/character';
import { nearby } from '@/state/nearby';
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
import { Predictor, wireState } from '../systems/prediction';

import { groundStyle, mapTiles } from './world-layout';

export interface WorldData {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
  /** After a MapChange: shown until the Snapshot confirms it. */
  readonly arrival?: { readonly x: number; readonly y: number };
}

const FADE_MS = 250;
const BANNER_MS = 1800;

function mapTitle(id: string): string {
  return id.charAt(0).toUpperCase() + id.slice(1).replace(/_/g, ' ');
}

type Wire = Data<'snapshot'>['players'][number];
type MobWire = Data<'snapshot'>['mobs'][number];
type DropWire = Data<'snapshot'>['drops'][number];

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
  private placed = false; // set by the first Snapshot that names the local player

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
  private hinted: number | null = null;

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
    if (this.entry.arrival) {
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
    this.pickup = new PickupSystem(() => socket.sendAction('pickUp'));
    const portals = new PortalSystem(() => socket.sendAction('enterPortal'));
    this.portals = portals;
    this.numberLayer = new DamageNumberLayer(this);
    this.attackGate = new AttackGate(classCooldownMs(this.entry.cls), () =>
      socket.sendAction('attack'),
    );
    const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
    this.sender = sender;
    sender.setCaptured(ui.getState().captureKeyboard);
    this.offs.push(
      bindKeyboard({
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
            case 'party':
              ui.getState().toggleWindow('party');
              return;
            case 'chat':
              ui.getState().openChat();
              return;
            default:
              return;
          }
        },
      }),
      ui.subscribe((s) => sender.setCaptured(s.captureKeyboard)),
      socket.on('snapshot', (m) =>
        this.onSnapshot(m.d.players, m.d.mobs, m.d.drops, this.tickOf(m.tick)),
      ),
      socket.on('delta', (m) =>
        this.onDelta(
          m.d.players,
          m.d.removedPlayers,
          m.d.mobs,
          m.d.removedMobs,
          m.d.drops,
          m.d.removedDrops,
          m.d.ackSeq,
          this.tickOf(m.tick),
        ),
      ),
      socket.on('correction', (m) => this.onCorrection(m.d)),
      socket.on('welcome', (m) => this.onWelcome(m.d.mapId)),
      socket.on('damage', (m) => this.onDamage(m.d)),
      socket.on('died', (m) => this.onTransition({ t: 'died', d: m.d })),
      socket.on('respawned', (m) => this.onTransition({ t: 'respawned', d: m.d })),
      socket.on('mapChange', (m) => this.onTransition({ t: 'mapChange', d: m.d })),
      socket.on('levelUp', () => this.onLevelUp()),
    );
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
    if (!predictor || !sender || !socket || !this.placed) {
      return;
    }
    const dead = this.transitionState.dead;
    for (const step of this.clock.advance(deltaMs)) {
      if (dead) {
        continue; // the server holds a dead player
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
    this.remotes?.applySnapshot(tick * TICK_MS, players);
    this.mobs.applySnapshot(tick * TICK_MS, mobs);
    const changed = this.drops.applySnapshot(drops);
    this.applyDrops(changed.added, changed.removed);
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

  /** After a reconnect: the same map waits for the next Snapshot; another map restarts the scene. */
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
    this.hinted = null;
    this.numbers.clear();
    this.numberLayer?.sync([]);
  }

  private inPortal(): boolean {
    const p = this.predictor;
    const v = this.view;
    return !!p && !!v && this.placed && standsIn(p.state.x, p.state.y, v.portals) !== undefined;
  }

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

  private arrow(x: number, y: number, facing: 1 | -1): void {
    const streak = this.add.rectangle(x, y, 16, 3, 0xf0ead6).setDepth(5);
    this.tweens.add({
      targets: streak,
      x: x + facing * ARROW_RANGE,
      duration: ARROW_MS,
      onComplete: () => streak.destroy(),
    });
  }

  private onDamage(d: Data<'damage'>): void {
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
