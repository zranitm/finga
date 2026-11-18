import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { keyboardLayout } from '@/state/keyboard-layout';
import { resetStores } from '@/testing/reset-stores';

import { watchKeyboardLayout } from './keyboard-layout';

interface Layout {
  current: [string, string][];
}

/** Chrome's navigator.keyboard: getLayoutMap and nothing else, not an EventTarget. */
function chromeKeyboard(layout: Layout) {
  const keyboard = { getLayoutMap: vi.fn(() => Promise.resolve(new Map(layout.current))) };
  vi.stubGlobal('navigator', { ...navigator, keyboard });
  return keyboard;
}

/** A keyboard that also fires layoutchange, as the API's draft has it. */
function listeningKeyboard(layout: Layout) {
  const keyboard = Object.assign(new EventTarget(), {
    getLayoutMap: vi.fn(() => Promise.resolve(new Map(layout.current))),
  });
  vi.stubGlobal('navigator', { ...navigator, keyboard });
  return keyboard;
}

const printedQ = () => keyboardLayout.getState().printed?.get('KeyQ');

describe('watchKeyboardLayout', () => {
  beforeEach(() => {
    resetStores();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('on Chrome’s keyboard, which has no listeners, reads at start and again when the window regains focus, until stopped', async () => {
    const layout: Layout = { current: [['KeyQ', 'q']] };
    const keyboard = chromeKeyboard(layout);
    const stop = watchKeyboardLayout();
    await vi.waitFor(() => expect(printedQ()).toBe('q'));
    layout.current = [['KeyQ', 'a']];
    window.dispatchEvent(new Event('focus'));
    await vi.waitFor(() => expect(printedQ()).toBe('a'));
    stop();
    window.dispatchEvent(new Event('focus'));
    await Promise.resolve();
    expect(keyboard.getLayoutMap).toHaveBeenCalledTimes(2);
  });

  it('where the keyboard fires layoutchange, reads again on it, until stopped', async () => {
    const layout: Layout = { current: [['KeyQ', 'q']] };
    const keyboard = listeningKeyboard(layout);
    const stop = watchKeyboardLayout();
    await vi.waitFor(() => expect(printedQ()).toBe('q'));
    layout.current = [['KeyQ', 'a']];
    keyboard.dispatchEvent(new Event('layoutchange'));
    await vi.waitFor(() => expect(printedQ()).toBe('a'));
    stop();
    keyboard.dispatchEvent(new Event('layoutchange'));
    await Promise.resolve();
    expect(keyboard.getLayoutMap).toHaveBeenCalledTimes(2);
  });

  it.each([
    { name: 'no keyboard API', keyboard: undefined },
    { name: 'a keyboard without getLayoutMap', keyboard: {} },
    {
      name: 'a getLayoutMap that refuses',
      keyboard: { getLayoutMap: () => Promise.reject(new Error('not allowed')) },
    },
    {
      name: 'a getLayoutMap that throws',
      keyboard: {
        getLayoutMap: () => {
          throw new Error('not allowed');
        },
      },
    },
  ])('$name keeps the US names and never throws', async ({ keyboard }) => {
    vi.stubGlobal('navigator', { ...navigator, keyboard });
    let stop: () => void = () => undefined;
    expect(() => {
      stop = watchKeyboardLayout();
    }).not.toThrow();
    expect(() => window.dispatchEvent(new Event('focus'))).not.toThrow();
    await Promise.resolve();
    expect(keyboardLayout.getState().printed).toBeUndefined();
    expect(() => stop()).not.toThrow();
  });
});
