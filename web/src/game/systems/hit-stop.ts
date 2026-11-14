// Hit-stop: the player's own hit holds their pose a moment, then the pose catches up so it is still back to ready at its ready point. It holds the drawing only, never a cancel.
const HIT_STOP_MS = 50;
const GAP_MS = 100; // at most one hold starts in this long, so a skill hitting three foes holds once

/** A pose held from `from` until `until`, then caught up by the action's ready point. */
export interface Hold {
  readonly from: number;
  readonly until: number;
  readonly readyAt: number;
}

/** The time a pose is drawn at for now: held, then running fast to meet the clock at the ready point, and the clock after. */
export function heldTime(h: Hold | null, now: number): number {
  if (!h || now < h.from || now >= h.readyAt) {
    return now;
  }
  if (now < h.until) {
    return h.from;
  }
  return h.from + ((now - h.until) * (h.readyAt - h.from)) / (h.readyAt - h.until);
}

/** One player's holds, for their hits. */
export class HitStops {
  private last: Hold | null = null;

  /** A hit at now in the action whose ready point is readyAt: a new hold, the one still holding for it, or none. */
  hit(now: number, readyAt: number): Hold | null {
    const l = this.last;
    if (l && now - l.from < GAP_MS) {
      return l.readyAt === readyAt && now < l.until ? l : null;
    }
    if (now >= readyAt) {
      return null; // past the ready point a hold would make ready late
    }
    this.last = { from: now, until: Math.min(now + HIT_STOP_MS, readyAt), readyAt };
    return this.last;
  }
}
