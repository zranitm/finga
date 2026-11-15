import { describe, expect, it } from 'vitest';

import { cameraAt, easeShare, heldIn } from './follow';

describe('cameraAt', () => {
  it('sits 16 m back from its target, 50° down, to the south', () => {
    const { x, y, z } = cameraAt(2, 1, 3, { x: 0, y: 0, z: 0 });
    expect(x).toBe(2);
    expect(Math.hypot(y - 1, z - 3)).toBeCloseTo(16);
    expect((Math.atan2(y - 1, 3 - z) * 180) / Math.PI).toBeCloseTo(50);
  });
});

describe('easeShare', () => {
  it('closes 0.15 of the gap in a 60 fps frame, and as much over the same time in smaller frames', () => {
    expect(easeShare(1000 / 60)).toBeCloseTo(0.15, 10);
    let gap = 1;
    for (let i = 0; i < 4; i++) {
      gap *= 1 - easeShare(1000 / 240);
    }
    expect(1 - gap).toBeCloseTo(0.15, 10);
  });
});

describe('heldIn', () => {
  it('holds a view inside the map, and centres one wider than it', () => {
    expect([heldIn(3, 72, 12), heldIn(40, 72, 12), heldIn(70, 72, 12)]).toEqual([12, 40, 60]);
    expect(heldIn(1, 12, 9)).toBe(6);
  });
});
