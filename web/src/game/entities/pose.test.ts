import { describe, expect, it } from 'vitest';

import { climbMoving, poseFor } from './pose';

describe('poseFor', () => {
  it.each([
    {
      name: 'standing still',
      s: { vx: 0, vy: 0, on_ground: true, climbing: false },
      pose: 'stand',
    },
    {
      name: 'walking right',
      s: { vx: 200, vy: 0, on_ground: true, climbing: false },
      pose: 'walk',
    },
    {
      name: 'walking left',
      s: { vx: -200, vy: 0, on_ground: true, climbing: false },
      pose: 'walk',
    },
    { name: 'rising', s: { vx: 0, vy: -620, on_ground: false, climbing: false }, pose: 'jump' },
    {
      name: 'falling while moving',
      s: { vx: 200, vy: 300, on_ground: false, climbing: false },
      pose: 'jump',
    },
    { name: 'on a rope', s: { vx: 0, vy: 0, on_ground: false, climbing: true }, pose: 'climb' },
    {
      name: 'climbing up',
      s: { vx: 0, vy: -140, on_ground: false, climbing: true },
      pose: 'climb',
    },
  ] as const)('$name shows $pose', ({ s, pose }) => {
    expect(poseFor(s)).toBe(pose);
  });

  it.each([
    {
      name: 'holding the rope',
      s: { vx: 0, vy: 0, on_ground: false, climbing: true },
      moving: false,
    },
    {
      name: 'climbing down',
      s: { vx: 0, vy: 140, on_ground: false, climbing: true },
      moving: true,
    },
    {
      name: 'falling, not on a rope',
      s: { vx: 0, vy: 140, on_ground: false, climbing: false },
      moving: false,
    },
  ])('$name animates: $moving', ({ s, moving }) => {
    expect(climbMoving(s)).toBe(moving);
  });
});
