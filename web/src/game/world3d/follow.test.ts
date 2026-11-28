import { describe, expect, it } from 'vitest';

import { terrain, height } from '@/sim/terrain';

import {
  CAMERA_CLEARANCE_M,
  cameraAt,
  clearShare,
  easeShare,
  heldView,
  pulledShare,
} from './follow';

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

describe('clearShare', () => {
  // 17 × 17 samples 8 m apart, 128 m square: a 30 m wall of ground over z 0 to 24 falling to flat ground by z 32
  const heights = Array.from({ length: 17 * 17 }, (_, i) => (Math.floor(i / 17) <= 3 ? 30 : 0));
  const ground = terrain(8, 17, 17, heights, 0.7);
  const groundAt = (x: number, z: number) => height(ground, x, z);

  it('leaves a camera that is over the ground where it is, and pulls one inside it in along its line, to above the ground, far zoom at a region’s south edge', () => {
    const farAt = (z: number) => {
      const look = { x: 60, y: 0.9, z };
      return { look, cam: cameraAt(look.x, look.y, look.z, 1, { x: 0, y: 0, z: 0 }) };
    };
    const open = farAt(100); // the camera 33 m south, over flat ground
    expect(clearShare(open.look, open.cam, groundAt)).toBe(1);
    const edge = farAt(50); // the camera at z 17, 23 m up, in the wall's 30 m
    expect(groundAt(edge.cam.x, edge.cam.z)).toBeGreaterThan(edge.cam.y);
    const share = clearShare(edge.look, edge.cam, groundAt);
    expect(share).toBeGreaterThan(0.4);
    expect(share).toBeLessThan(0.8);
    const at = {
      x: edge.look.x + (edge.cam.x - edge.look.x) * share,
      y: edge.look.y + (edge.cam.y - edge.look.y) * share,
      z: edge.look.z + (edge.cam.z - edge.look.z) * share,
    };
    expect(at.y).toBeGreaterThanOrEqual(groundAt(at.x, at.z) + CAMERA_CLEARANCE_M - 0.05);
    expect(at.y).toBeLessThan(groundAt(at.x, at.z) + CAMERA_CLEARANCE_M + 0.6); // no farther in than it must
  });

  it('pulls in at once and eases back out as the zoom does', () => {
    expect(pulledShare(1, 0.5, 0.15)).toBe(0.5);
    expect(pulledShare(0.5, 1, 0.15)).toBeCloseTo(0.575, 10);
    expect(pulledShare(1, 1, 0.15)).toBe(1);
  });
});
