import { describe, expect, it } from 'vitest';

import { greyPixels } from './grey-art';

describe('greyPixels', () => {
  it('makes each pixel grey by its luminance, the lightest opaque one white', () => {
    // A light green, a dark green, and a white one that is fully transparent.
    const p = new Uint8ClampedArray([120, 200, 60, 255, 40, 80, 20, 255, 255, 255, 255, 0]);
    greyPixels(p);
    expect([p[0], p[1], p[2], p[3]]).toEqual([255, 255, 255, 255]);
    expect(p[4]).toBe(p[5]);
    expect(p[5]).toBe(p[6]);
    expect(p[4]).toBeGreaterThan(80);
    expect(p[4]).toBeLessThan(110);
    expect(p[11]).toBe(0);
  });

  it('leaves an image with nothing opaque as it was', () => {
    const p = new Uint8ClampedArray([0, 0, 0, 0]);
    greyPixels(p);
    expect([...p]).toEqual([0, 0, 0, 0]);
  });
});
