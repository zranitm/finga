// What the app holds of the game, so it never imports a renderer.

/** The world a character enters: its map and tick rate, and who plays. */
export interface WorldEntry {
  readonly mapId: string;
  readonly characterId: string;
  readonly cls: string;
  readonly name: string;
  /** The world's ticks per second. */
  readonly tickRateHz: number;
}

/** A canvas mounted in parent, a world entered and left. */
export interface GameView {
  attach(parent: HTMLElement): () => void;
  enter(entry: WorldEntry): void;
  leave(): void;
}
