import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { keyboardLayout } from '@/state/keyboard-layout';
import { resetStores } from '@/testing/reset-stores';

import { watchKeyboardLayout } from './keyboard-layout';

/** A browser keyboard with the Keyboard Map API, printing what layout says. */
function keyboardPrinting(layout: { current: [string, string][] }) {
  const keyboard = Object.assign(new EventTarget(), {
    getLayoutMap: vi.fn(() => Promise.resolve(new Map(layout.current))),
  });
  vi.stubGlobal('navigator', { ...navigator, keyboard });
  return keyboard;
}

describe('watchKeyboardLayout', () => {
  beforeEach(() => {
    resetStores();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads what the keyboard prints, and again when the player switches layouts, until stopped', async () => {
    const layout = { current: [['KeyQ', 'q']] as [string, string][] };
    const keyboard = keyboardPrinting(layout);
    const stop = watchKeyboardLayout();
    await vi.waitFor(() => expect(keyboardLayout.getState().printed?.get('KeyQ')).toBe('q'));
    layout.current = [['KeyQ', 'a']];
    keyboard.dispatchEvent(new Event('layoutchange'));
    await vi.waitFor(() => expect(keyboardLayout.getState().printed?.get('KeyQ')).toBe('a'));
    stop();
    layout.current = [['KeyQ', 'z']];
    keyboard.dispatchEvent(new Event('layoutchange'));
    await Promise.resolve();
    expect(keyboard.getLayoutMap).toHaveBeenCalledTimes(2);
  });

  it('a browser without the API, or one refusing it, keeps the US names', async () => {
    vi.stubGlobal('navigator', { ...navigator, keyboard: undefined });
    expect(() => watchKeyboardLayout()()).not.toThrow();
    const keyboard = keyboardPrinting({ current: [] });
    keyboard.getLayoutMap.mockImplementation(() => Promise.reject(new Error('not allowed')));
    watchKeyboardLayout();
    await Promise.resolve();
    expect(keyboardLayout.getState().printed).toBeUndefined();
  });
});
