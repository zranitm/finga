// Hit-stop: the player's own hit holds their pose a moment, then the pose catches up so ready still lands on the cancel point.
export const HIT_STOP_MS = 50;
const GAP_MS = 100; // at most one hold starts in this long, so a skill hitting three foes holds once

/** A pose held from `from` until `until`, then caught up by the action's cancel point. */
export interface Hold {
  readonly from: number;
  readonly until: number;
  readonly cancelAt: number;
}

/** The time a pose is drawn at for now: held, then running fast to meet the clock at the cancel point, and the clock after. */
export function heldTime(h: Hold | null, now: number): number {
  if (!h || now < h.from || now >= h.cancelAt) {
    return now;
  }
  if (now < h.until) {
    return h.from;
  }
  return h.from + ((now - h.until) * (h.cancelAt - h.from)) / (h.cancelAt - h.until);
}

/** One player's holds, for their hits. */
export class HitStops {
  private last: Hold | null = null;

  /** A hit at now in the action whose cancel point is cancelAt: a new hold, the one still holding for it, or none. */
  hit(now: number, cancelAt: number): Hold | null {
    const l = this.last;
    if (l && now - l.from < GAP_MS) {
      return l.cancelAt === cancelAt && now < l.until ? l : null;
    }
    if (now >= cancelAt) {
      return null; // past the cancel point a hold would make ready late
    }
    this.last = { from: now, until: Math.min(now + HIT_STOP_MS, cancelAt), cancelAt };
    return this.last;
  }
}
