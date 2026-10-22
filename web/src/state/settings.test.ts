import { beforeEach, describe, expect, it } from 'vitest';

import { resetStores } from '@/testing/reset-stores';

import { cachePreset, loadCachedPreset, PRESET_KEY, settings } from './settings';

describe('settings store', () => {
  beforeEach(() => {
    localStorage.clear();
    resetStores();
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

  it('a throwing or missing storage still gives Classic and loses nothing else', () => {
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('full');
      },
    };
    expect(loadCachedPreset(broken)).toBe('classic');
    expect(() => cachePreset('wasd', broken)).not.toThrow();
    expect(loadCachedPreset(null)).toBe('classic');
    expect(() => cachePreset('wasd', null)).not.toThrow();
  });
});
