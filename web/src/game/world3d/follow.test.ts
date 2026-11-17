import { describe, expect, it } from 'vitest';

import { cameraAt, easeShare, heldView } from './follow';

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

describe('heldView', () => {
  it('holds the view inside the map, 5.18 m deep either side and as wide as its aspect, and centres it on a side narrower than that', () => {
    const at = (x: number, z: number, sizeX: number, sizeZ: number) => {
      const v = heldView({ x, y: 2, z }, sizeX, sizeZ, 1.6, { x: 0, y: 0, z: 0 });
      return [v.x, v.y, v.z];
    };
    expect(at(1, 0.5, 72, 36)).toEqual([expect.closeTo(8.291, 3), 2, expect.closeTo(5.182, 3)]);
    expect(at(40, 20, 72, 36)).toEqual([40, 2, 20]);
    expect(at(71, 35.5, 72, 36)).toEqual([expect.closeTo(63.709, 3), 2, expect.closeTo(30.818, 3)]);
    expect(at(1, 1, 12, 8)).toEqual([6, 2, 4]);
  });
});
