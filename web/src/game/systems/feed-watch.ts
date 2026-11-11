// The world sends a frame every tick, so a long silence on an open socket means the world has stalled.
export class FeedWatch {
  private last: number | null = null;
  private fresh = true;

  constructor(private readonly quietMs: number) {}

  /** A frame arrived; the next check counts from then. */
  heard(): void {
    this.fresh = true;
  }

  /** Whether no frame has arrived for longer than quietMs, by the scene's clock; the first check starts the count. */
  stalled(now: number): boolean {
    if (this.fresh || this.last === null) {
      this.last = now;
      this.fresh = false;
    }
    return now - this.last > this.quietMs;
  }
}
