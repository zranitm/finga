import { describe, expect, it } from 'vitest';

import { RingBuffer } from './ring-buffer';

function filled(capacity: number, items: number[]): RingBuffer<number> {
  const b = new RingBuffer<number>(capacity);
  for (const item of items) {
    b.push(item);
  }
  return b;
}

describe('RingBuffer', () => {
  it.each([
    {
      name: 'under capacity keeps everything in order',
      capacity: 4,
      pushed: [1, 2, 3],
      want: [1, 2, 3],
    },
    { name: 'at capacity keeps everything', capacity: 3, pushed: [1, 2, 3], want: [1, 2, 3] },
    {
      name: 'past capacity keeps the newest',
      capacity: 3,
      pushed: [1, 2, 3, 4, 5],
      want: [3, 4, 5],
    },
    { name: 'twice around', capacity: 2, pushed: [1, 2, 3, 4, 5], want: [4, 5] },
    { name: 'empty', capacity: 3, pushed: [], want: [] },
  ])('$name', ({ capacity, pushed, want }) => {
    const b = filled(capacity, pushed);
    expect([...b]).toEqual(want);
    expect(b.size).toBe(want.length);
    expect(b.last()).toBe(want.at(-1));
  });

  it.each([
    { name: 'a hit', wanted: 3, want: 3 },
    { name: 'a miss', wanted: 9, want: undefined },
    { name: 'an overwritten item', wanted: 1, want: undefined },
  ])('find by key: $name', ({ wanted, want }) => {
    const b = filled(3, [1, 2, 3, 4]);
    expect(b.find((n) => n, wanted)).toBe(want);
  });

  it('retain drops the rejected items and keeps the order', () => {
    const b = filled(4, [1, 2, 3, 4, 5]);
    b.retain((n) => n % 2 === 1);
    expect([...b]).toEqual([3, 5]);
    b.push(6);
    expect([...b]).toEqual([3, 5, 6]);
  });

  it('clear empties it', () => {
    const b = filled(3, [1, 2]);
    b.clear();
    expect(b.size).toBe(0);
    expect(b.last()).toBeUndefined();
  });

  it.each([0, -1, 1.5])('refuses capacity %s', (capacity) => {
    expect(() => new RingBuffer(capacity)).toThrow(RangeError);
  });
});
