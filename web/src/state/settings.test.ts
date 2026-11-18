import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const KEYS = 'finga.settings.keys';

/** The store as a fresh page load makes it, from what this browser cached. */
async function freshSettings() {
  vi.resetModules();
  return (await import('./settings')).settings;
}

describe('settings: the keys', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('a page starts on the keys this browser last played on, before the account’s arrive', async () => {
    (await freshSettings()).getState().setKeys({ attack: 'KeyV' });
    expect(localStorage.getItem(KEYS)).toBe('{"attack":"KeyV"}');
    expect((await freshSettings()).getState().keys).toEqual({ attack: 'KeyV' });
  });

  it.each([
    { name: 'nothing cached', cached: null },
    { name: 'not JSON', cached: '{attack' },
    { name: 'a list', cached: '["KeyV"]' },
    { name: 'a key that isn’t a string', cached: '{"attack":3}' },
  ])('$name starts on the defaults', async ({ cached }) => {
    if (cached !== null) {
      localStorage.setItem(KEYS, cached);
    }
    expect((await freshSettings()).getState().keys).toEqual({});
  });

  it('a throwing or missing storage still plays, on the defaults, and keeps a change for this page', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('full');
    });
    const s = await freshSettings();
    expect(s.getState().keys).toEqual({});
    s.getState().setKeys({ attack: 'KeyV' });
    expect(s.getState().keys).toEqual({ attack: 'KeyV' });
    vi.stubGlobal('localStorage', undefined);
    expect((await freshSettings()).getState().keys).toEqual({});
  });
});
