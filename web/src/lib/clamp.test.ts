import { describe, expect, it } from 'vitest';

import { clamp } from './clamp';

describe('clamp', () => {
  it.each([
    { name: 'inside the range', value: 5, min: 0, max: 10, want: 5 },
    { name: 'below the minimum', value: -1, min: 0, max: 10, want: 0 },
    { name: 'above the maximum', value: 11, min: 0, max: 10, want: 10 },
    { name: 'on the edges', value: 10, min: 0, max: 10, want: 10 },
    { name: 'a point range', value: 3, min: 2, max: 2, want: 2 },
  ])('$name', ({ value, min, max, want }) => {
    expect(clamp(value, min, max)).toBe(want);
  });

  it('refuses an inverted range', () => {
    expect(() => clamp(1, 5, 0)).toThrow(RangeError);
  });
});
