import { afterEach, describe, expect, it, vi } from 'vitest';

import { idempotencyKey, requestId, uuidV4 } from './id';

const v4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe('ids', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it.each([
    { name: 'all zero bytes', fill: 0x00, want: '00000000-0000-4000-8000-000000000000' },
    { name: 'all one bits', fill: 0xff, want: 'ffffffff-ffff-4fff-bfff-ffffffffffff' },
  ])('sets the version and variant bits over $name', ({ fill, want }) => {
    expect(uuidV4((b) => b.fill(fill))).toBe(want);
  });

  it('works with crypto.randomUUID removed, as in an insecure context', () => {
    const randomUUID = vi.spyOn(crypto, 'randomUUID').mockImplementation(() => {
      throw new TypeError('randomUUID is not available here');
    });
    const id = uuidV4();
    expect(id).toMatch(v4);
    expect(randomUUID).not.toHaveBeenCalled();
  });

  it('makes distinct random IDs with the version and variant bits', () => {
    const ids = new Set(Array.from({ length: 200 }, () => uuidV4()));
    expect(ids.size).toBe(200);
    for (const id of ids) {
      expect(id).toMatch(v4);
    }
  });

  it.each([
    { name: 'a request ID', make: requestId, prefix: 'r-' },
    { name: 'an idempotency key', make: idempotencyKey, prefix: 'k-' },
  ])('$name carries its prefix', ({ make, prefix }) => {
    const id = make();
    expect(id.startsWith(prefix)).toBe(true);
    expect(id.slice(2)).toMatch(v4);
  });

  it('refuses a random source that gives the wrong length', () => {
    expect(() => uuidV4(() => new Uint8Array(4))).toThrow(RangeError);
  });
});
