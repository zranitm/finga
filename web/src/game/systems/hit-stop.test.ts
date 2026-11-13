import { describe, expect, it } from 'vitest';

import { HIT_STOP_MS, HitStops, heldTime } from './hit-stop';

const CANCEL_AT = 1750;

describe('hit-stop', () => {
  it('holds the pose 50 ms, then catches up to meet the clock at the cancel point', () => {
    const h = new HitStops().hit(1100, CANCEL_AT)!;
    expect(h).toEqual({ from: 1100, until: 1100 + HIT_STOP_MS, cancelAt: CANCEL_AT });
    expect([1099, 1100, 1120, 1149].map((t) => heldTime(h, t))).toEqual([1099, 1100, 1100, 1100]);
    expect(heldTime(h, 1150)).toBe(1100);
    expect(heldTime(h, 1450)).toBeCloseTo(1100 + (300 * 650) / 600);
    expect([CANCEL_AT, CANCEL_AT + 1, 2400].map((t) => heldTime(h, t))).toEqual([
      CANCEL_AT,
      CANCEL_AT + 1,
      2400,
    ]);
  });

  it.each([1000, 1300, 1600, 1720, 1749])(
    'never shifts the cancel point: a hold from %i reaches it exactly then, and not a moment before',
    (from) => {
      const h = new HitStops().hit(from, CANCEL_AT)!;
      let was = -Infinity;
      for (let t = from - 10; t < CANCEL_AT; t += 0.5) {
        const drawn = heldTime(h, t);
        expect(drawn).toBeLessThan(CANCEL_AT);
        expect(drawn).toBeGreaterThanOrEqual(was);
        was = drawn;
      }
      expect(heldTime(h, CANCEL_AT)).toBe(CANCEL_AT);
    },
  );

  it('a hold close to the cancel point holds only up to it', () => {
    expect(new HitStops().hit(1730, CANCEL_AT)).toEqual({
      from: 1730,
      until: CANCEL_AT,
      cancelAt: CANCEL_AT,
    });
  });

  it('none at or past the cancel point', () => {
    expect(new HitStops().hit(CANCEL_AT, CANCEL_AT)).toBeNull();
    expect(heldTime(null, 1234)).toBe(1234);
  });

  it('starts at most one hold per 100 ms: hits within its 50 ms share it, later ones none', () => {
    const stops = new HitStops();
    const h = stops.hit(1000, CANCEL_AT);
    expect(stops.hit(1000, CANCEL_AT)).toBe(h); // a skill's second foe, in the same frame
    expect(stops.hit(1049, CANCEL_AT)).toBe(h);
    expect(stops.hit(1050, CANCEL_AT)).toBeNull();
    expect(stops.hit(1099, CANCEL_AT)).toBeNull();
    expect(stops.hit(1100, CANCEL_AT)).toEqual({ from: 1100, until: 1150, cancelAt: CANCEL_AT });
  });

  it("a hold doesn't carry over to the next action", () => {
    const stops = new HitStops();
    stops.hit(1000, CANCEL_AT);
    expect(stops.hit(1020, 1900)).toBeNull(); // cancelled into another action within the gap
  });
});
