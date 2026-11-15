import { describe, expect, it } from 'vitest';

import { cameraAt, turn, yawOf } from './follow';

describe('turn', () => {
  it('turns the short way round', () => {
    expect(turn(3, -3, 1)).toBeCloseTo(-3 + 2 * Math.PI);
    expect(turn(0, yawOf(1, 0), 0.5)).toBeCloseTo(Math.PI / 4);
  });
});

describe('cameraAt', () => {
  it('sits 16 m back from its target, 50° down, to the south', () => {
    const [x, y, z] = cameraAt(2, 1, 3);
    expect(x).toBe(2);
    expect(Math.hypot(y - 1, z - 3)).toBeCloseTo(16);
    expect((Math.atan2(y - 1, 3 - z) * 180) / Math.PI).toBeCloseTo(50);
  });
});
