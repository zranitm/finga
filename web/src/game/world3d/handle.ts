// The 3D world behind the game's handle (3d-world.md section 10): Babylon loads only once a world is entered.
import type { GameView } from '../main';
import type { WorldEntry } from '../scenes/world';

export class World3DHandle implements GameView {
  private parent: HTMLElement | null = null;
  private entry: WorldEntry | null = null;
  private close: (() => void) | null = null;
  private visit = 0; // each open and close starts the next; a stage that loads late for an old one closes at once

  attach(parent: HTMLElement): () => void {
    this.parent = parent;
    this.open();
    return () => {
      this.shut();
      if (this.parent === parent) {
        this.parent = null;
      }
    };
  }

  enter(entry: WorldEntry): void {
    this.entry = entry;
    this.open();
  }

  leave(): void {
    this.entry = null;
    this.shut();
  }

  private open(): void {
    this.shut();
    const { parent, entry } = this;
    if (!parent || !entry) {
      return;
    }
    const visit = this.visit;
    void import('./run')
      .then(({ run }) => run(parent, entry))
      .then((close) => {
        if (visit === this.visit) {
          this.close = close;
        } else {
          close();
        }
      });
  }

  private shut(): void {
    this.visit++;
    this.close?.();
    this.close = null;
  }
}
