import { describe, expect, it } from 'vitest';

import { skillById } from '@/progression/skills';

import { skillTiming } from '../systems/actions';
import type { ActionTiming } from '../systems/actions';

import { beatsOf } from './beats';

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
    });
  });

  it('a hit after the start lands at its tick', () => {
    const b = beatsOf({ ...swing, hit: 0.2 }, 0, 50); // the hit at tick 4
    expect(b.hit).toBe(200);
  });

  it('a skill’s look fills it: Strike’s strike over the first third of its second, then the follow-through to its end', () => {
    const t = skillTiming(skillById('strike')!.stages[0]!, 50);
    const b = beatsOf(t, 1000, 50);
    expect([b.hit, b.strikeEnd, b.readyAt, b.end]).toEqual([1000, 1000 + 1000 / 3, 2000, 2000]);
  });
});
