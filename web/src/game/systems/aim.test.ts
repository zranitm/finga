import { describe, expect, it } from 'vitest';

import { aimYaw } from './aim';
import type { Mark } from './aim';

const EAST = Math.PI / 2;
const mark = (id: number, x: number, z: number): Mark => ({
  id,
  x,
  z,
  radius: 0.5,
  hp: 10,
  inView: true,
  inFrame: true,
});

describe('aim', () => {
  it('turns to the mark, behind included, or keeps the heading with none or with it underfoot', () => {
    expect(aimYaw(0, 0, EAST, mark(1, 0, -2))).toBeCloseTo(Math.PI);
    expect(aimYaw(0, 0, EAST, mark(1, -2, 0))).toBeCloseTo(-EAST);
    expect(aimYaw(0, 0, EAST, null)).toBe(EAST);
    expect(aimYaw(1, 1, EAST, mark(1, 1, 1))).toBe(EAST);
  });
});
