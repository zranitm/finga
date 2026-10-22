import { afterEach, describe, expect, it } from 'vitest';

import { clearToken, getToken, setToken } from './token';

describe('token holder', () => {
  afterEach(() => {
    clearToken();
  });

  it('starts empty, holds what it is given, and forgets on clear', () => {
    expect(getToken()).toBeNull();
    setToken('tok-1');
    expect(getToken()).toBe('tok-1');
    setToken('tok-2');
    expect(getToken()).toBe('tok-2');
    clearToken();
    expect(getToken()).toBeNull();
  });

  it('refuses an empty token', () => {
    expect(() => setToken('')).toThrow(/empty/);
  });
});
