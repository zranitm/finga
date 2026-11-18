import { afterEach, describe, expect, it, vi } from 'vitest';

import { readStored, writeStored } from './storage';

describe('storage', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it('keeps a value per browser', () => {
    writeStored('finga.test', 'yes');
    expect(readStored('finga.test')).toBe('yes');
    expect(readStored('finga.none')).toBeNull();
  });

  it('a blocked or full storage reads as nothing and drops the write, never throwing', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full');
    });
    expect(readStored('finga.test')).toBeNull();
    expect(() => writeStored('finga.test', 'yes')).not.toThrow();
  });

  it('no storage at all reads as nothing', () => {
    vi.stubGlobal('localStorage', undefined);
    expect(readStored('finga.test')).toBeNull();
    expect(() => writeStored('finga.test', 'yes')).not.toThrow();
  });
});
