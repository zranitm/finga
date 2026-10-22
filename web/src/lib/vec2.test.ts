import { describe, expect, it } from 'vitest';

import { add, distance, length, scale } from './vec2';

describe('vec2', () => {
  it.each([
    { name: 'add', got: add({ x: 1, y: 2 }, { x: 3, y: -5 }), want: { x: 4, y: -3 } },
    { name: 'add the zero vector', got: add({ x: 1, y: 2 }, { x: 0, y: 0 }), want: { x: 1, y: 2 } },
    { name: 'scale', got: scale({ x: 1.5, y: -2 }, 2), want: { x: 3, y: -4 } },
    { name: 'scale by zero', got: scale({ x: 7, y: 9 }, 0), want: { x: 0, y: 0 } },
  ])('$name', ({ got, want }) => {
    expect(got).toEqual(want);
  });

  it.each([
    { name: 'a 3-4-5 triangle', v: { x: 3, y: 4 }, want: 5 },
    { name: 'zero length', v: { x: 0, y: 0 }, want: 0 },
    { name: 'a negative component', v: { x: -2, y: 0 }, want: 2 },
  ])('length of $name', ({ v, want }) => {
    expect(length(v)).toBe(want);
  });

  it.each([
    { name: 'two points', a: { x: 1, y: 1 }, b: { x: 4, y: 5 }, want: 5 },
    { name: 'the same point', a: { x: 2, y: 3 }, b: { x: 2, y: 3 }, want: 0 },
    { name: 'either order', a: { x: 4, y: 5 }, b: { x: 1, y: 1 }, want: 5 },
  ])('distance between $name', ({ a, b, want }) => {
    expect(distance(a, b)).toBe(want);
  });
});
