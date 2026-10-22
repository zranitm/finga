// World: draws the map named in Welcome from its object layers and the
// local player, predicted one tick ahead of the server (C08): a fixed
// 1/60 s accumulator runs sim/step with the Input sent for each tick, and
// every Delta is reconciled against the prediction. C09 adds the other
// players, C10 the mobs. The scene stays thin: it calls the systems.
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
import { bindKeyboard } from '../systems/keyboard';
import { Predictor } from '../systems/prediction';

import { groundStyle, mapTiles } from './world-layout';

/** What the app passes when the player enters the world, from Welcome. */
export interface WorldData {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
}

type Wire = Data<'Snapshot'>['players'][number];

export class World extends Phaser.Scene {
  private entry: WorldData = { mapId: 'town', characterId: '', cls: 'warrior' };
  private player: PlayerView | null = null;
  private background: Phaser.GameObjects.Image | null = null;
  private offs: (() => void)[] = [];

  private socket: GameSocket | null = null;
  private predictor: Predictor | null = null;
  private sender: InputSender | null = null;
  private readonly clock = new FixedStep();
  private placed = false; // the first Snapshot has named the local player

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
    this.player = new PlayerView(this, this.entry.cls);
    this.player.apply({
      x: view.player_spawn.x,
      y: view.player_spawn.y,
      vx: 0,
      vy: 0,
      on_ground: true,
      climbing: false,
      facing: 1,
    });
    this.player.sprite.setVisible(false);
    cam.setBounds(0, 0, view.width, view.height);
    cam.startFollow(this.player.sprite, true, 0.15, 0.15);

    const socket = this.registry.get(REGISTRY.socket) as GameSocket;
    this.socket = socket;
    this.predictor = new Predictor(view.geometry);
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
      socket.on('Snapshot', (m) => this.onSnapshot(m.d.players)),
      socket.on('Delta', (m) => this.onDelta(m.d.players, m.d.ack_seq)),
      socket.on('Correction', (m) => this.onCorrection(m.d)),
    );
  }

  /** The frame loop: whole sub-steps only; a tick boundary samples and sends the keys. */
  override update(_time: number, deltaMs: number): void {
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
  }

  private me(players: readonly Wire[]): Wire | undefined {
    return players.find((p) => p.id === this.entry.characterId);
  }

  /** A Snapshot is the truth: the first one places the player, a later one (a reconnect) resets. */
  private onSnapshot(players: readonly Wire[]): void {
    const me = this.me(players);
    if (!me || !this.predictor || !this.player) {
      return;
    }
    this.predictor.reset(me);
    this.placed = true;
    this.player.sprite.setVisible(true);
    this.player.apply(this.predictor.state);
    this.pushVitals(me);
  }

  /** A Correction (reserved on page 4) has no facing; the current one stays. */
  private onCorrection(c: Data<'Correction'>): void {
    if (this.predictor && this.placed) {
      this.predictor.correct({ ...c, facing: this.predictor.state.facing }, c.seq);
    }
  }

  private onDelta(players: readonly Wire[], ackSeq: number): void {
    const me = this.me(players);
    if (!me || !this.predictor || !this.placed) {
      return;
    }
    this.predictor.reconcile(me, ackSeq);
    this.pushVitals(me);
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
    this.player = null;
    this.background = null;
    this.socket = null;
    this.predictor = null;
    this.sender = null;
    this.placed = false;
  }
}
