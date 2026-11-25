// What the app holds of the game, so it never imports a renderer.
import type { Picks } from '@/state/wardrobe';

/** The world a character enters: its map and tick rate, and who plays. */
export interface WorldEntry {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
  /** The world's ticks per second. */
  readonly tickRateHz: number;
}

/** A character drawn in 3D into a canvas of its own, as the wardrobe shows one: dressed in a class and picks, turned by a drag, drawn each frame till it's closed. */
export interface Portrait {
  show(cls: string, look: Picks): void;
  turn(radians: number): void;
  close(): void;
}

/** A canvas mounted in parent, a world entered and left. */
export interface GameView {
  attach(parent: HTMLElement): () => void;
  enter(entry: WorldEntry): void;
  leave(): void;
  /** A portrait drawn into canvas while a world is open; one asked for with none draws nothing. */
  portrait(canvas: HTMLCanvasElement): Portrait;
}
