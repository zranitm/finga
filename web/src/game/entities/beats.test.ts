import { describe, expect, it } from 'vitest';

import type { ActionTiming } from '../systems/actions';

import { beatsOf, cueAt, poseAt } from './beats';

// A Beginner's basic attack: 21 ticks of 50 ms, the hit at its start, the cancel point at ⌊21 × 0.85⌋ = 17.
const swing: ActionTiming = { kind: 'attack', ticks: 21, hit: 0, cancel: 0.85, cancelable: true };

describe('beats', () => {
  it('strike from the hit for a third of the way to the cancel point, then the follow-through, ready at the cancel point', () => {
    const b = beatsOf(swing, 1000, 50);
    expect(b).toEqual({ start: 1000, hit: 1000, strikeEnd: 1000 + 850 / 3, cancelAt: 1850 });
    expect(poseAt(b, 999)).toBeNull();
    expect(poseAt(b, 1000)).toEqual({ hitFrame: true, lean: 1, lunge: 1, stretch: 1 });
    expect(poseAt(b, 1000 + 850 / 6)?.stretch).toBeCloseTo(0.5);
    const follow = poseAt(b, b.strikeEnd);
    expect(follow).toEqual({ hitFrame: false, lean: 1, lunge: 1, stretch: 0 });
    const late = poseAt(b, 1849);
    expect(late?.hitFrame).toBe(false);
    expect(late?.lean).toBeLessThan(0.01);
    expect(poseAt(b, 1850)).toBeNull();
  });

  it('a hit after the start winds up first, leaning back until it', () => {
    const b = beatsOf({ ...swing, hit: 0.2 }, 0, 50); // the hit at tick 4
    expect(b.hit).toBe(200);
    expect(poseAt(b, 100)).toEqual({ hitFrame: false, lean: -0.5, lunge: 0, stretch: 0 });
    expect(poseAt(b, 200)?.hitFrame).toBe(true);
  });

  it("the cue comes early_press before the cancel point, never before the action's start", () => {
    expect(cueAt(beatsOf(swing, 1000, 50))).toBe(1750);
    expect(cueAt(beatsOf({ ...swing, ticks: 1 }, 1000, 50))).toBe(1000); // its cancel point is a tick in
  });
});
