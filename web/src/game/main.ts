// The game config and the handle the app drives it through: attach the
// canvas once, enter the world when Welcome arrives, leave it on the way
// out. The handle exists before the canvas does, so the app can hold it.
import Phaser from 'phaser';

import type { GameSocket } from '@/net/socket';

import { SCENES } from './keys';
import { REGISTRY } from './registry';
import { Boot } from './scenes/boot';
import { Preloader } from './scenes/preloader';
import { World } from './scenes/world';
import type { WorldData } from './scenes/world';

export interface GameDeps {
  readonly socket: GameSocket;
}

export function gameConfig(parent: HTMLElement): Phaser.Types.Core.GameConfig {
  return {
    type: Phaser.AUTO,
    parent,
    backgroundColor: '#0a0c16',
    scale: { mode: Phaser.Scale.RESIZE, width: '100%', height: '100%' },
    scene: [Boot, Preloader, World],
  };
}

export class GameHandle {
  private game: Phaser.Game | null = null;
  private pending: WorldData | null = null;

  constructor(private readonly deps: GameDeps) {}

  /** Creates the game in the parent element; returns the function that destroys it. */
  attach(parent: HTMLElement): () => void {
    const game = new Phaser.Game(gameConfig(parent));
    game.registry.set(REGISTRY.socket, this.deps.socket);
    this.game = game;
    if (this.pending) {
      game.scene.start(SCENES.World, this.pending);
    }
    return () => {
      if (this.game === game) {
        this.game = null;
      }
      game.destroy(true);
    };
  }

  /** Starts the World scene for the map and character from Welcome; restarts it if it is running. */
  enter(data: WorldData): void {
    this.pending = data;
    const game = this.game;
    if (!game) {
      return;
    }
    if (game.scene.isActive(SCENES.World)) {
      game.scene.stop(SCENES.World);
    }
    game.scene.start(SCENES.World, data);
  }

  /** Stops the World scene; the canvas stays behind the React screens. */
  leave(): void {
    this.pending = null;
    this.game?.scene.stop(SCENES.World);
  }

  get attached(): boolean {
    return this.game !== null;
  }
}
