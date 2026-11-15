import { describe, expect, it } from 'vitest';

import { turn } from './angle';

describe('turn', () => {
  it('turns the short way round', () => {
    expect(turn(3, -3, 1)).toBeCloseTo(-3 + 2 * Math.PI);
    expect(turn(-3, 3, 0.5)).toBeCloseTo(-Math.PI);
    expect(turn(0, Math.PI / 2, 0.5)).toBeCloseTo(Math.PI / 4);
  });
});
