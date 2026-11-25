// Which other players are named over their heads (large-regions.md section 10): the nearest few to the camera's target, held so a crowd's last name doesn't flicker.

const HOLD_M = 2; // a named player keeps their name till another comes this much nearer than them

/** Where a body stands. */
interface At {
  readonly x: number;
  readonly z: number;
}

/** The players named among those shown: every one while they're no more than the limit, else the limit nearest, a named one counted HOLD_M nearer than they stand. */
export class Named {
  private readonly held = new Set<string>();

  constructor(private readonly limit: number) {}

  /** Names the players of shown for this frame, from at. */
  pick(shown: ReadonlyMap<string, At>, at: At): void {
    if (shown.size <= this.limit) {
      this.held.clear(); // refilled, not made anew, and nothing sorted
      for (const id of shown.keys()) {
        this.held.add(id);
      }
      return;
    }
    const ranked = [...shown]
      .map(
        ([id, s]) =>
          [id, Math.hypot(s.x - at.x, s.z - at.z) - (this.held.has(id) ? HOLD_M : 0)] as const,
      )
      .sort((a, b) => a[1] - b[1]);
    this.held.clear();
    for (const [id] of ranked.slice(0, this.limit)) {
      this.held.add(id);
    }
  }

  /** Whether player id is named. */
  has(id: string): boolean {
    return this.held.has(id);
  }
}
