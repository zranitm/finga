import { describe, expect, it } from 'vitest';

import { FeedWatch } from './feed-watch';

describe('FeedWatch', () => {
  it('counts from its first check, and a silence past the limit is a stall', () => {
    const w = new FeedWatch(2000);
    expect(w.stalled(500)).toBe(false);
    expect(w.stalled(2500)).toBe(false);
    expect(w.stalled(2501)).toBe(true);
  });

  it('a frame ends the stall at the next check, and the count starts again from it', () => {
    const w = new FeedWatch(2000);
    w.stalled(0);
    expect(w.stalled(3000)).toBe(true);
    w.heard();
    expect(w.stalled(3050)).toBe(false);
    expect(w.stalled(5050)).toBe(false);
    expect(w.stalled(5051)).toBe(true);
  });

  it('frames that arrive while the scene is not drawn, as in a hidden tab, still count', () => {
    const w = new FeedWatch(2000);
    w.stalled(0);
    w.heard(); // the socket delivered while no update ran
    expect(w.stalled(60_000)).toBe(false);
  });
});
