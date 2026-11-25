// A figure drawn into a window's canvas, as the game hands one out, its calls counted.
import { vi } from 'vitest';

export function fakePortrait() {
  return { show: vi.fn(), turn: vi.fn(), close: vi.fn() };
}
