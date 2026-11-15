// The 3D world behind the game's handle (3d-world.md section 10): Babylon loads only once a world is entered, and the map's messages are held from the moment it is.
import type { GameSocket } from '@/net/socket';
import { toasts } from '@/state/toasts';

import { Arrival } from '../systems/arrival';
import type { GameView, WorldEntry } from '../view';

export class World3DHandle implements GameView {
  private parent: HTMLElement | null = null;
  private entry: WorldEntry | null = null;
  private arriving: Arrival | null = null; // the entered map's messages, held until a world takes them
  private close: (() => void) | null = null;
  private visit = 0; // each open and close starts the next; code that loads for an old one opens nothing

  constructor(private readonly socket: GameSocket) {}

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
    this.arriving?.take();
    this.arriving = new Arrival(this.socket, entry.mapId, entry.tickRateHz);
    this.entry = entry;
    this.open();
  }

  leave(): void {
    this.arriving?.take();
    this.arriving = null;
    this.entry = null;
    this.shut();
  }

  /** Opens the world on the entry; a world opened again starts holding the map's messages afresh. */
  private open(): void {
    this.shut();
    const { parent, entry } = this;
    if (!parent || !entry) {
      return;
    }
    const arriving = this.arriving ?? new Arrival(this.socket, entry.mapId, entry.tickRateHz);
    this.arriving = null;
    const visit = this.visit;
    import('./run').then(
      ({ run }) => {
        if (visit !== this.visit) {
          arriving.take(); // left before the code came
          return;
        }
        this.close = run(parent, entry, arriving, this.socket);
      },
      (err: unknown) => {
        arriving.take();
        console.info(
          `finga: the 3D world didn't load: ${err instanceof Error ? err.message : String(err)}`,
        );
        toasts.getState().push('The game didn’t load. Reload the page to try again.');
      },
    );
  }

  private shut(): void {
    this.visit++;
    this.close?.();
    this.close = null;
  }
}
