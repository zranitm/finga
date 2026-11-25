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
  // kept from frame to frame, so a crowd's frame makes no new arrays
  private readonly ids: string[] = [];
  private readonly keys: number[] = [];
  private readonly order: number[] = [];
  private readonly byKey = (a: number, b: number): number => this.keys[a]! - this.keys[b]!;

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
    let n = 0;
    let kept = 0;
    let farthestHeld = -Infinity;
    let nearestOther = Infinity;
    for (const id of shown.keys()) {
      const s = shown.get(id)!;
      const d = Math.hypot(s.x - at.x, s.z - at.z);
      const named = this.held.has(id);
      const key = named ? d - HOLD_M : d;
      if (named) {
        kept++;
        farthestHeld = Math.max(farthestHeld, key);
      } else {
        nearestOther = Math.min(nearestOther, key);
      }
      this.ids[n] = id;
      this.keys[n] = key;
      this.order[n] = n;
      n++;
    }
    if (kept === this.limit && farthestHeld <= nearestOther) {
      return; // nobody crossed the band, so the names stand unsorted
    }
    this.ids.length = n;
    this.keys.length = n;
    this.order.length = n;
    this.order.sort(this.byKey);
    this.held.clear();
    for (let i = 0; i < this.limit; i++) {
      this.held.add(this.ids[this.order[i]!]!);
    }
  }

  /** Whether player id is named. */
  has(id: string): boolean {
    return this.held.has(id);
  }
}
