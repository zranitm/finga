import { describe, expect, it } from 'vitest';

import { stackTags } from './name-tag';
import type { TagBox } from './name-tag';

// The Warrior Trainer stands at 1120 on the town's ground at 608.
const tag = (x: number, w = 60, feet = 608): TagBox => ({ x, bottom: feet - 70, w, h: 17 });
const marker = (x: number): TagBox => ({ x, bottom: 608 - 88, w: 12, h: 26 });
const trainer = tag(1120, 100);
// A lifted tag sits 2 px clear of the text under it.
const above = (b: TagBox) => b.bottom - b.h - 2;

describe('stackTags', () => {
  it('tags that cover nothing stay where they are', () => {
    expect(stackTags([trainer], [tag(1020), tag(1240)])).toEqual([538, 538]);
  });

  it('a player on a trainer has its tag lifted just above the trainer’s', () => {
    expect(stackTags([trainer], [tag(1110)])).toEqual([above(trainer)]);
  });

  it('a tag lifted into the trainer’s marker goes on above the marker', () => {
    expect(stackTags([trainer, marker(1120)], [tag(1110)])).toEqual([above(marker(1120))]);
  });

  // A box the tag only reaches once lifted, so a single pass in this order would miss it.
  it('a tag climbs past every box its lift runs into, whatever their order', () => {
    const high: TagBox = { x: 1120, bottom: 516, w: 12, h: 26 };
    expect(stackTags([high, trainer], [tag(1110)])).toEqual([above(high)]);
  });

  it('players on one spot stack, the earlier tag lower', () => {
    const [first, second, third] = stackTags([trainer], [tag(1110), tag(1130), tag(1100)]);
    expect(first).toBe(above(trainer));
    expect(second).toBe(above({ ...tag(1130), bottom: first ?? 0 }));
    expect(third).toBe(above({ ...tag(1100), bottom: second ?? 0 }));
  });

  it('the earlier tag keeps its place and the later one lifts', () => {
    const [mine, theirs] = stackTags([], [tag(400), tag(410)]);
    expect(mine).toBe(538);
    expect(theirs).toBe(above(tag(400)));
  });

  it('tags only touching at their edges both stay', () => {
    expect(stackTags([], [tag(400), tag(460)])).toEqual([538, 538]);
  });

  it('a tag a little lower than another still lifts above it, never down onto its owner', () => {
    const higher = tag(400, 60, 600);
    expect(stackTags([higher], [tag(410)])).toEqual([above(higher)]);
  });

  it('a tag already clear above a box stays', () => {
    expect(stackTags([trainer], [tag(1120, 60, 560)])).toEqual([490]);
  });
});
