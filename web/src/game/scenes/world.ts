// World: draws the map named in Welcome from its object layers, the local
// player predicted one tick ahead of the server (C08), and the other
// players interpolated 100 ms behind it (C09). A Welcome after a reconnect
// reloads the map when it changed and waits for the next Snapshot. C10
// adds the mobs. The scene stays thin: it calls the systems.
import Phaser from 'phaser';

import { FixedStep } from '@/lib/fixed-step';
import type { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';
import { character } from '@/state/character';
import { settings } from '@/state/settings';
import { ui } from '@/state/ui';

import { PlayerView } from '../entities/player';
import { ART_SCALE, SCENES, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';
import { InputSender } from '../systems/input-sender';
import { RemotePlayers, RenderClock, TICK_MS } from '../systems/interpolation';
import { bindKeyboard } from '../systems/keyboard';
import { Predictor } from '../systems/prediction';

import { groundStyle, mapTiles } from './world-layout';

/** What the app passes when the player enters the world, from Welcome. */
export interface WorldData {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
}

type Wire = Data<'Snapshot'>['players'][number];

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

  constructor() {
    super(SCENES.World);
  }

  init(data: WorldData): void {
    this.entry = data;
    this.placed = false;
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
    const style = groundStyle(view.id);
    const cam = this.cameras.main;

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
    cam.setBounds(0, 0, view.width, view.height);
    cam.startFollow(this.player.sprite, true, 0.15, 0.15);

    const socket = this.registry.get(REGISTRY.socket) as GameSocket;
    this.socket = socket;
    this.predictor = new Predictor(view.geometry);
    this.remotes = new RemotePlayers(this.entry.characterId);
    this.renderClock.reset();
    const sender = new InputSender({ send: (input, tick) => socket.sendInput(input, tick) });
    this.sender = sender;
    sender.setCaptured(ui.getState().captureKeyboard);
    this.offs.push(
      bindKeyboard({
        target: sender,
        preset: () => settings.getState().preset,
        captured: () => ui.getState().captureKeyboard,
      }),
      ui.subscribe((s) => sender.setCaptured(s.captureKeyboard)),
      socket.on('Snapshot', (m) => this.onSnapshot(m.d.players, this.tickOf(m.tick))),
      socket.on('Delta', (m) =>
        this.onDelta(m.d.players, m.d.removed_players, m.d.ack_seq, this.tickOf(m.tick)),
      ),
      socket.on('Correction', (m) => this.onCorrection(m.d)),
      socket.on('Welcome', (m) => this.onWelcome(m.d.map_id)),
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
    for (const step of this.clock.advance(deltaMs)) {
      if (step.tickStart) {
        const input = sender.tickStart();
        predictor.beginTick(socket.sequence, input);
      }
      predictor.subStep();
    }
    this.player?.apply(predictor.state);
    this.drawRemotes(time);
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

  private me(players: readonly Wire[]): Wire | undefined {
    return players.find((p) => p.id === this.entry.characterId);
  }

  /** A Snapshot is the truth: it places the local player and replaces the other players. */
  private onSnapshot(players: readonly Wire[], tick: number): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applySnapshot(tick * TICK_MS, players);
    const me = this.me(players);
    if (!me || !this.predictor || !this.player) {
      return;
    }
    this.predictor.reset(me);
    this.placed = true;
    this.player.setVisible(true);
    this.player.apply(this.predictor.state);
    this.pushVitals(me);
  }

  private onDelta(
    players: readonly Wire[],
    removed: readonly string[],
    ackSeq: number,
    tick: number,
  ): void {
    this.renderClock.update(tick, this.time.now);
    this.remotes?.applyDelta(tick * TICK_MS, players, removed);
    const me = this.me(players);
    if (!me || !this.predictor || !this.placed) {
      return;
    }
    this.predictor.reconcile(me, ackSeq);
    this.pushVitals(me);
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
    this.remotes?.clear();
    this.renderClock.reset();
    for (const v of this.remoteViews.values()) {
      v.destroy();
    }
    this.remoteViews.clear();
  }

  /** HP into the character store only when it changed (the store checks); MP is not on the wire. */
  private pushVitals(me: Wire): void {
    const c = character.getState().character;
    if (c && c.hp !== me.hp) {
      character.getState().setVitals(me.hp, c.mp);
    }
  }

  private teardown(): void {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
    for (const v of this.remoteViews.values()) {
      v.destroy();
    }
    this.remoteViews.clear();
    this.remotes = null;
    this.player = null;
    this.background = null;
    this.socket = null;
    this.predictor = null;
    this.sender = null;
    this.placed = false;
  }
}
