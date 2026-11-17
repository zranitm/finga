// Target lock (target-lock.md sections 3 and 12): the mob the player's attacks and skills aim at, kept between actions; the nearest on screen is locked when none is, and Tab moves it on.
import type { Mark } from './aim';

/** How far from the feet a mob's centre is, across the ground. */
function across(x: number, z: number, m: Mark): number {
  return Math.hypot(m.x - x, m.z - z);
}

/** The living mobs the lock may pick, those in view, nearest the feet (x, z) first, a tie to the lower id. */
function pickable(x: number, z: number, mobs: Iterable<Mark>): Mark[] {
  const near: { readonly m: Mark; readonly d: number }[] = [];
  for (const m of mobs) {
    if (m.hp > 0 && m.inView) {
      near.push({ m, d: across(x, z, m) });
    }
  }
  return near.sort((a, b) => a.d - b.d || a.m.id - b.m.id).map((n) => n.m);
}

export class Lock {
  private id: number | null = null;

  /** The locked mob among mobs, seen from feet (x, z): kept while it lives anywhere on screen, else the nearest one in view, else none. */
  update(x: number, z: number, mobs: readonly Mark[]): Mark | null {
    const held = mobs.find((m) => m.id === this.id);
    if (held && held.hp > 0 && held.inFrame) {
      return held;
    }
    const nearest = pickable(x, z, mobs)[0] ?? null;
    this.id = nearest?.id ?? null;
    return nearest;
  }

  /** Tab: the next mob in view after the locked one, nearest first, round to the nearest after the farthest; the nearest when none in view is locked. With none in view, the lock holds as update keeps it. */
  next(x: number, z: number, mobs: readonly Mark[]): Mark | null {
    const order = pickable(x, z, mobs);
    if (order.length === 0) {
      return this.update(x, z, mobs);
    }
    const at = order.findIndex((m) => m.id === this.id);
    const m = order[(at + 1) % order.length]!;
    this.id = m.id;
    return m;
  }

  /** Lets go of the lock, as at a death, a respawn, a map change or a rejoin; the next update picks again. */
  clear(): void {
    this.id = null;
  }
}
