import type { Data } from '@/protocol/messages';

export type DropWire = Data<'snapshot'>['drops'][number];

export interface Point {
  readonly x: number;
  readonly y: number;
}

/** What changed on the ground: drops that appeared, went without an owner taking them, or were collected. */
export interface DropChanges {
  readonly added: DropWire[];
  readonly removed: DropWire[];
  readonly collected: DropWire[];
}

/** How a drop leaves the screen: flying to its owner, or fading where it lies. */
export type Departure = { readonly kind: 'fly'; readonly to: Point } | { readonly kind: 'fade' };

/** A collected drop flies to its owner when they are on the map; one gone otherwise, or whose owner is elsewhere, fades. */
export function departure(collected: boolean, owner: Point | undefined): Departure {
  return collected && owner ? { kind: 'fly', to: owner } : { kind: 'fade' };
}

export class DropTracker {
  private readonly drops = new Map<number, DropWire>();

  /** A whole map: the drops not in it went, as on a rejoin. */
  applySnapshot(drops: readonly DropWire[]): DropChanges {
    const kept = new Set(drops.map((d) => d.id));
    const gone = [...this.drops.keys()].filter((id) => !kept.has(id));
    return this.applyDelta(drops, gone, []);
  }

  applyDelta(
    drops: readonly DropWire[],
    removed: readonly number[],
    collected: readonly number[],
  ): DropChanges {
    const added: DropWire[] = [];
    for (const d of drops) {
      if (!this.drops.has(d.id)) {
        added.push(d);
      }
      this.drops.set(d.id, d);
    }
    return { added, removed: this.take(removed), collected: this.take(collected) };
  }

  private take(ids: readonly number[]): DropWire[] {
    const out: DropWire[] = [];
    for (const id of ids) {
      const d = this.drops.get(id);
      if (d) {
        this.drops.delete(id);
        out.push(d);
      }
    }
    return out;
  }

  clear(): void {
    this.drops.clear();
  }
}
