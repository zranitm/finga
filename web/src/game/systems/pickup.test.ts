import { describe, expect, it, vi } from 'vitest';

import { DropTracker, PICKUP_RANGE, PickupSystem, boxCentre } from './pickup';
import type { DropWire } from './pickup';

const gel = (id: number, x: number, y = 608): DropWire => ({
  id,
  template: 'slime_gel',
  quantity: 1,
  x,
  y,
});

describe('DropTracker', () => {
  it('a Snapshot sets the drops and reports what appeared and what went; Deltas add and remove', () => {
    const t = new DropTracker();
    expect(t.applySnapshot([gel(1, 100), gel(2, 200)])).toEqual({
      added: [gel(1, 100), gel(2, 200)],
      removed: [],
    });
    expect(t.applyDelta([gel(3, 300), gel(1, 100)], [2, 99])).toEqual({
      added: [gel(3, 300)],
      removed: [2],
    });
    expect(t.size).toBe(2);
    expect(t.applySnapshot([gel(3, 300)])).toEqual({ added: [], removed: [1] });
    expect(t.get(3)?.x).toBe(300);
    t.clear();
    expect(t.size).toBe(0);
  });

  it.each([
    { name: 'a drop right at the centre', cx: 100, cy: 580, want: 1 },
    { name: 'a drop 40 px away is in reach', cx: 100 + PICKUP_RANGE, cy: 608, want: 1 },
    { name: 'a drop 41 px away is not', cx: 100 + PICKUP_RANGE + 1, cy: 608, want: undefined },
    { name: 'the closer of two', cx: 190, cy: 608, want: 2 },
  ])('$name', ({ cx, cy, want }) => {
    const t = new DropTracker();
    t.applySnapshot([gel(1, 100, 580), gel(2, 200), gel(1, 100, 580)]);
    t.applySnapshot([gel(1, 100, 608), gel(2, 200)]);
    expect(t.nearest(cx, cy)?.id).toBe(want);
  });

  it('the box centre sits half the player height above the feet', () => {
    expect(boxCentre(416, 608)).toEqual({ x: 416, y: 580 });
  });
});

describe('PickupSystem', () => {
  it('the key within range sends one PickUp; pressing it twice in one tick sends one', () => {
    const send = vi.fn(() => true);
    const p = new PickupSystem(send);
    expect(p.press(true, 10)).toBe(true);
    expect(p.press(true, 10)).toBe(false);
    expect(send).toHaveBeenCalledTimes(1);
    expect(p.press(true, 11)).toBe(true);
    expect(send).toHaveBeenCalledTimes(2);
  });

  it('out of range sends nothing', () => {
    const send = vi.fn(() => true);
    const p = new PickupSystem(send);
    expect(p.press(false, 10)).toBe(false);
    expect(send).not.toHaveBeenCalled();
    expect(p.press(true, 10)).toBe(true); // the tick was not used up by the refused press
  });
});
