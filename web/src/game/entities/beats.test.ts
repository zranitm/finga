import { describe, expect, it } from 'vitest';

import { actionPoints } from '@/progression/actions';
import { skillById } from '@/progression/skills';

import { skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';

import { beatsOf, poseAt } from './beats';

// A Beginner's basic attack: 21 ticks of 50 ms, the hit at its start, the cancel point at ⌊21 × 0.3⌋ = 6, and back to ready at ⌊21 × 0.85⌋ = 17.
const swing: ActionTiming = {
  kind: 'attack',
  ticks: 21,
  hit: 0,
  cancel: 0.3,
  ready: 0.85,
  cancelable: true,
};

describe('beats', () => {
  it('strike from the hit for a third of the way to the ready point, then the follow-through, ready at the ready point, well past the cancel point', () => {
    const b = beatsOf(swing, 1000, 50);
    expect(b).toEqual({
      start: 1000,
      hit: 1000,
      strikeEnd: 1000 + 850 / 3,
      readyAt: 1850,
      end: 2050,
      roots: false,
    });
    expect(poseAt(b, 999)).toBeNull();
    expect(poseAt(b, 1000)).toEqual({
      hitFrame: true,
      lean: 1,
      lunge: 1,
      stretch: 1,
    });
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
    expect(poseAt(b, 100)).toEqual({
      hitFrame: false,
      lean: -0.5,
      lunge: 0,
      stretch: 0,
    });
    expect(poseAt(b, 200)?.hitFrame).toBe(true);
  });

  it('a skill’s look fills its root: Strike’s strike over the first third of its second, then the follow-through to its end, still drawn where an attack may cut it at 450 ms', () => {
    const t = skillTiming(skillById('strike')!.stages[0]!, 50);
    const b = beatsOf(t, 1000, 50);
    expect([b.hit, b.strikeEnd, b.readyAt, b.end, b.roots]).toEqual([
      1000,
      1000 + 1000 / 3,
      2000,
      2000,
      true,
    ]);
    const cut = poseAt(b, 1000 + actionPoints(t.ticks, t.hit, t.cancel).cancel * 50);
    expect(cut?.hitFrame).toBe(false);
    expect(cut?.lean).toBeGreaterThan(0.5); // in the follow-through, which the cut leaves as an afterimage
    expect(poseAt(b, 1999)?.lean).toBeGreaterThan(0);
    expect(poseAt(b, 2000)).toBeNull();
  });

  it('a skill whose ready point comes sooner holds a slight recovery lean from it to its end', () => {
    // Made up: ready at 9 of 30 ticks, 450 ms in, as the cancelable skills were before the root.
    const b = beatsOf(
      { kind: 'skill', ticks: 30, hit: 0, cancel: 0.3, ready: 0.3, cancelable: true },
      1000,
      50,
    );
    expect([b.readyAt, b.end, b.roots]).toEqual([1450, 2500, true]);
    expect(poseAt(b, 1000)).toMatchObject({ hitFrame: true });
    expect(poseAt(b, 1449)?.lean).toBeCloseTo(0.25, 2);
    expect(poseAt(b, 1450)).toEqual({
      hitFrame: false,
      lean: 0.25,
      lunge: 0,
      stretch: 0,
    });
    expect(poseAt(b, 2499)).toMatchObject({ lean: 0.25 });
    expect(poseAt(b, 2500)).toBeNull();
  });
});
