// Drops and the pickup key (C11) over plain data: the drops on the map
// from Snapshot and Delta, which one is within reach, and one PickUp per
// tick when the key is pressed in range. The inventory itself changes only
// on InventoryChanged; the server ignores an out-of-range pickup, which is
// what the range hint is for.
import type { Data } from '@/protocol/messages';
import { params } from '@/sim/step';

export type DropWire = Data<'Snapshot'>['drops'][number];

/** The player's box centre must be within this of the drop (page 5). */
export const PICKUP_RANGE = 40;

/** The centre of the player's box from the feet position (sim/step's x, y). */
export function boxCentre(x: number, y: number): { x: number; y: number } {
  return { x, y: y - params.height / 2 };
}

export class DropTracker {
  private readonly drops = new Map<number, DropWire>();

  applySnapshot(drops: readonly DropWire[]): { added: DropWire[]; removed: number[] } {
    const removed = [...this.drops.keys()].filter((id) => !drops.some((d) => d.id === id));
    for (const id of removed) {
      this.drops.delete(id);
    }
    return { added: this.applyDelta(drops, []).added, removed };
  }

  applyDelta(
    drops: readonly DropWire[],
    removed: readonly number[],
  ): { added: DropWire[]; removed: number[] } {
    const added: DropWire[] = [];
    for (const d of drops) {
      if (!this.drops.has(d.id)) {
        added.push(d);
      }
      this.drops.set(d.id, d);
    }
    const gone: number[] = [];
    for (const id of removed) {
      if (this.drops.delete(id)) {
        gone.push(id);
      }
    }
    return { added, removed: gone };
  }

  /** The closest drop within PICKUP_RANGE of a point, or undefined. */
  nearest(cx: number, cy: number): DropWire | undefined {
    let best: DropWire | undefined;
    let bestD = PICKUP_RANGE;
    for (const d of this.drops.values()) {
      const dist = Math.hypot(d.x - cx, d.y - cy);
      if (dist <= bestD) {
        best = d;
        bestD = dist;
      }
    }
    return best;
  }

  get(id: number): DropWire | undefined {
    return this.drops.get(id);
  }

  get size(): number {
    return this.drops.size;
  }

  clear(): void {
    this.drops.clear();
  }
}

/** The pickup key: one PickUp per tick, only with a drop in range. */
export class PickupSystem {
  private sentTick = -1;

  constructor(private readonly send: () => boolean) {}

  /** True when a PickUp went out for this press. */
  press(inRange: boolean, tick: number): boolean {
    if (!inRange || tick === this.sentTick) {
      return false;
    }
    this.sentTick = tick;
    return this.send();
  }
}
