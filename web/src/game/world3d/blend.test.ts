import { describe, expect, it } from 'vitest';

import { BLEND_MS, Fades, layered } from './blend';

describe('Fades', () => {
  it('swings a weight fully in BLEND_MS, linearly', () => {
    const f = new Fades();
    f.aim('walk', 1);
    f.step(BLEND_MS / 4);
    expect(f.get('walk')).toBeCloseTo(0.25);
    f.step(BLEND_MS);
    expect(f.get('walk')).toBe(1);
  });

  it('fades the others out as one fades in', () => {
    const f = new Fades();
    f.only('idle');
    f.step(BLEND_MS);
    f.only('walk');
    f.step(BLEND_MS / 2);
    expect(f.get('idle')).toBeCloseTo(0.5);
    expect(f.get('walk')).toBeCloseTo(0.5);
    f.step(BLEND_MS / 2);
    expect(f.entries()).toEqual([
      ['idle', 0],
      ['walk', 1],
    ]);
  });
});

describe('layered', () => {
  const sums = (m: ReturnType<typeof layered>) => {
    let upper = 0;
    let lower = 0;
    for (const w of m.values()) {
      upper += w.upper;
      lower += w.lower;
    }
    return { upper, lower };
  };

  it('plays an action on the whole body while standing', () => {
    const m = layered([['attack', 1]], [['idle', 1]], 0);
    expect(m.get('attack')).toEqual({ upper: 1, lower: 1 });
    expect(m.get('idle')).toEqual({ upper: 0, lower: 0 });
  });

  it('plays an action on the upper body alone while moving, the legs walking', () => {
    const m = layered([['attack', 1]], [['walk', 1]], 1);
    expect(m.get('attack')).toEqual({ upper: 1, lower: 0 });
    expect(m.get('walk')).toEqual({ upper: 0, lower: 1 });
  });

  it('keeps each layer whole through a cut and a mask switch', () => {
    const m = layered(
      [
        ['attack', 0.4],
        ['skill', 0.6],
      ],
      [
        ['idle', 0.3],
        ['walk', 0.7],
      ],
      0.5,
    );
    const s = sums(m);
    expect(s.upper).toBeCloseTo(1);
    expect(s.lower).toBeCloseTo(1);
    expect(m.get('skill')?.lower).toBeCloseTo(0.3);
  });

  it('scales actions summing past 1 down to it', () => {
    const m = layered(
      [
        ['attack', 1],
        ['skill', 1],
      ],
      [['idle', 1]],
      0,
    );
    expect(m.get('attack')?.upper).toBeCloseTo(0.5);
    expect(m.get('idle')).toEqual({ upper: 0, lower: 0 });
  });
});
