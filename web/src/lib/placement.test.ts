import { describe, expect, it } from 'vitest';

import { besidePointer } from './placement';

const viewport = { width: 1000, height: 700 };
const card = { width: 200, height: 150 };

describe('besidePointer', () => {
  it.each([
    {
      name: 'room below and right: there, past the pointer',
      pointer: { x: 100, y: 100 },
      want: { x: 116, y: 116 },
    },
    {
      name: 'near the right edge: flipped left of the pointer',
      pointer: { x: 900, y: 100 },
      want: { x: 684, y: 116 },
    },
    {
      name: 'near the bottom: flipped above the pointer',
      pointer: { x: 100, y: 650 },
      want: { x: 116, y: 484 },
    },
    {
      name: 'in the bottom-right corner: up and left',
      pointer: { x: 990, y: 690 },
      want: { x: 774, y: 524 },
    },
    {
      name: 'no room either side: pinned to the left edge',
      pointer: { x: 100, y: 100 },
      card: { width: 990, height: 150 },
      want: { x: 0, y: 116 },
    },
  ])('$name', ({ pointer, card: size = card, want }) => {
    expect(besidePointer(pointer, size, viewport)).toEqual(want);
  });

  it('a card larger than the viewport starts at its top-left corner', () => {
    expect(besidePointer({ x: 500, y: 300 }, { width: 2000, height: 900 }, viewport)).toEqual({
      x: 0,
      y: 0,
    });
  });

  it('a card that fits is always fully on screen, wherever the pointer is', () => {
    for (let x = 0; x <= viewport.width; x += 25) {
      for (let y = 0; y <= viewport.height; y += 25) {
        for (const size of [{ width: 1, height: 1 }, card, { width: 600, height: 500 }, viewport]) {
          const at = besidePointer({ x, y }, size, viewport);
          expect(at.x).toBeGreaterThanOrEqual(0);
          expect(at.y).toBeGreaterThanOrEqual(0);
          expect(at.x + size.width).toBeLessThanOrEqual(viewport.width);
          expect(at.y + size.height).toBeLessThanOrEqual(viewport.height);
        }
      }
    }
  });
});
