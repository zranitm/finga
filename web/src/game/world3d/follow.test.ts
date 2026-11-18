import { describe, expect, it } from 'vitest';

import { cameraAt, easeShare, heldView } from './follow';

describe('cameraAt', () => {
  it('sits 16 m back from its target, 50° down, to the south, and zoomed out 40 m back, 35° down, easing between', () => {
    const at = (zoom: number) => {
      const { x, y, z } = cameraAt(2, 1, 3, zoom, { x: 0, y: 0, z: 0 });
      return [x, Math.hypot(y - 1, z - 3), (Math.atan2(y - 1, 3 - z) * 180) / Math.PI];
    };
    expect(at(0)).toEqual([2, expect.closeTo(16), expect.closeTo(50)]);
    expect(at(1)).toEqual([2, expect.closeTo(40), expect.closeTo(35)]);
    expect(at(0.5)).toEqual([2, expect.closeTo(28), expect.closeTo(42.5)]);
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
    const at = (x: number, z: number, sizeX: number, sizeZ: number, zoom = 0) => {
      const v = heldView({ x, y: 2, z }, sizeX, sizeZ, 1.6, zoom, { x: 0, y: 0, z: 0 });
      return [v.x, v.y, v.z];
    };
    expect(at(1, 0.5, 72, 36)).toEqual([expect.closeTo(8.291, 3), 2, expect.closeTo(5.182, 3)]);
    expect(at(40, 20, 72, 36)).toEqual([40, 2, 20]);
    expect(at(71, 35.5, 72, 36)).toEqual([expect.closeTo(63.709, 3), 2, expect.closeTo(30.818, 3)]);
    expect(at(1, 1, 12, 8)).toEqual([6, 2, 4]);
    expect(at(1, 0.5, 1000, 1000, 1)).toEqual([
      expect.closeTo(15.52, 3),
      2,
      expect.closeTo(9.7, 3),
    ]); // zoomed out, it spans more
  });
});
