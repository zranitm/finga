// Where a card goes beside the pointer so that it stays fully on screen.
import { clamp } from './clamp';

export interface Point {
  readonly x: number;
  readonly y: number;
}

export interface Size {
  readonly width: number;
  readonly height: number;
}

/** Below and right of the pointer by gap; flipped left or up where that runs off the viewport, and pinned inside it when neither side fits. */
export function besidePointer(pointer: Point, card: Size, viewport: Size, gap = 16): Point {
  let x = pointer.x + gap;
  if (x + card.width > viewport.width) {
    x = pointer.x - gap - card.width;
  }
  let y = pointer.y + gap;
  if (y + card.height > viewport.height) {
    y = pointer.y - gap - card.height;
  }
  return {
    x: clamp(x, 0, Math.max(0, viewport.width - card.width)),
    y: clamp(y, 0, Math.max(0, viewport.height - card.height)),
  };
}
