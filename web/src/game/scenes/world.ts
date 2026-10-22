// World: draws the map named in Welcome from its object layers and the
// local player at the server's position. First cut (C07): the player is
// placed by the first Snapshot and follows the server's state; C08 adds
// prediction, C09 the other players, C10 the mobs.
import Phaser from 'phaser';

import type { GameSocket } from '@/net/socket';
import type { Data } from '@/protocol/messages';
import { parseMap } from '@/sim/map';

import { PlayerView } from '../entities/player';
import { ART_SCALE, SCENES, mapKey } from '../keys';
import { EVENTS, REGISTRY } from '../registry';

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

  constructor() {
    super(SCENES.World);
  }

  init(data: WorldData): void {
    this.entry = data;
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
    this.offs.push(
      socket.on('Snapshot', (m) => this.applyPlayers(m.d.players)),
      socket.on('Delta', (m) => this.applyPlayers(m.d.players)),
    );
  }

  private applyPlayers(players: readonly Wire[]): void {
    const me = players.find((p) => p.id === this.entry.characterId);
    if (me && this.player) {
      this.player.sprite.setVisible(true);
      this.player.apply(me);
    }
  }

  private teardown(): void {
    for (const off of this.offs) {
      off();
    }
    this.offs = [];
    this.player = null;
    this.background = null;
  }
}
