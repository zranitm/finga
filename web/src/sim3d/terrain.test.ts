import { describe, expect, it } from 'vitest';

import { height, terrain } from './terrain';

describe('terrain', () => {
  // As server/internal/sim3d's TestSteepMarks: steep along an axis when the rise across either edge along it exceeds max_slope × cell.
  it.each([
    ['a slope along x', [0, 0.4, 0, 0.4], true, false],
    ['a slope along z', [0, 0, 0.4, 0.4], false, true],
    ['one x edge over the limit', [0, 0.4, 0.2, 0.4], true, false],
    ['a rise of exactly max_slope × cell', [0, 0.35, 0, 0.35], false, false],
    [
      "a fall: the mark holds either way, and the move's way decides uphill",
      [0.4, 0, 0.4, 0],
      true,
      false,
    ],
    ['one raised corner: an x edge and a z edge', [0, 0, 0, 0.4], true, true],
  ] as const)('%s', (_, heights, steepX, steepZ) => {
    const t = terrain(0.5, 2, 2, heights, 0.7);
    expect([t.steepX[0], t.steepZ[0]]).toEqual([steepX, steepZ]);
  });

  it('is bilinear between samples', () => {
    const t = terrain(2, 2, 2, [0, 2, 4, 6], 0.7);
    const at = ([x, z]: readonly [number, number]) => height(t, x, z);
    expect(
      [
        [0, 0],
        [2, 0],
        [0, 2],
        [1, 1],
        [2, 2],
        [0.5, 1.5],
      ].map((p) => at(p as [number, number])),
    ).toEqual([0, 2, 4, 3, 6, 3.5]);
  });

  // As TestNegativeZeroKeepsItsSign: a -0 coordinate's cell fraction is -0 in Go, whose int() has no -0, and must be here too.
  it('keeps the sign of a -0 coordinate, as Go does', () => {
    const t = terrain(1, 2, 2, [-0, 1, -0, 1], 0.7);
    expect(height(t, -0, -0)).toBe(-0);
  });
});
