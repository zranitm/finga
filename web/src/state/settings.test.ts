import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { cachePreset, loadCachedPreset, PRESET_KEY, settings } from './settings';

describe('settings store', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('the preset round-trips through localStorage', () => {
    settings.getState().setPreset('wasd');
    expect(settings.getState().preset).toBe('wasd');
    expect(localStorage.getItem(PRESET_KEY)).toBe('wasd');
    expect(loadCachedPreset()).toBe('wasd');
  });

  it.each([
    { name: 'nothing cached', cached: null },
    { name: 'a value that is not a preset', cached: 'vim' },
    { name: 'an empty value', cached: '' },
  ])('$name falls back to Classic', ({ cached }) => {
    if (cached !== null) {
      localStorage.setItem(PRESET_KEY, cached);
    }
    expect(loadCachedPreset()).toBe('classic');
  });

  it('a throwing storage still gives Classic and loses nothing else', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full');
    });
    expect(loadCachedPreset()).toBe('classic');
    expect(() => cachePreset('wasd')).not.toThrow();
  });

  it('no storage at all still gives Classic', () => {
    vi.stubGlobal('localStorage', undefined);
    expect(loadCachedPreset()).toBe('classic');
    expect(() => cachePreset('wasd')).not.toThrow();
  });
});
