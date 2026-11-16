// Target lock (target-lock.md section 3): the mob the player's attacks and skills aim at, kept between actions; the nearest is locked when none is, and Tab moves it on.
import { actions } from '@/gamedata/actions';

import type { Mark } from './aim';

/** How far from the feet a mob's centre is, across the ground. */
function across(x: number, z: number, m: Mark): number {
  return Math.hypot(m.x - x, m.z - z);
}

/** The living mobs within the lock's range of feet (x, z), nearest first, a tie to the lower id. */
function inRange(x: number, z: number, mobs: Iterable<Mark>): Mark[] {
  const near: { readonly m: Mark; readonly d: number }[] = [];
  for (const m of mobs) {
    const d = across(x, z, m);
    if (m.hp > 0 && d <= actions.lock_range) {
      near.push({ m, d });
    }
  }
  return near.sort((a, b) => a.d - b.d || a.m.id - b.m.id).map((n) => n.m);
}

export class Lock {
  private id: number | null = null;

  /** The locked mob among mobs, seen from feet (x, z): kept while it lives within range, else the nearest such one, else none. */
  update(x: number, z: number, mobs: readonly Mark[]): Mark | null {
    const held = mobs.find((m) => m.id === this.id);
    if (held && held.hp > 0 && across(x, z, held) <= actions.lock_range) {
      return held;
    }
    const nearest = inRange(x, z, mobs)[0] ?? null;
    this.id = nearest?.id ?? null;
    return nearest;
  }

  /** Tab: the next mob in range after the locked one, nearest first, round to the nearest after the farthest; the nearest when none is locked. */
  next(x: number, z: number, mobs: readonly Mark[]): Mark | null {
    const order = inRange(x, z, mobs);
    const at = order.findIndex((m) => m.id === this.id);
    const m = order[(at + 1) % Math.max(1, order.length)] ?? null;
    this.id = m?.id ?? null;
    return m;
  }

  /** Lets go of the lock, as at a death, a respawn, a map change or a rejoin; the next update picks again. */
  clear(): void {
    this.id = null;
  }
}
