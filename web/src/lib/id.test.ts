import { afterEach, describe, expect, it, vi } from 'vitest';

import { idempotencyKey, requestId, uuidV7 } from './id';

const v7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

const bytes = (fill: number) => (b: Uint8Array<ArrayBuffer>) => b.fill(fill);
const at = (ms: number) => () => ms;

describe('ids', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('matches the UUIDv7 example in RFC 9562', () => {
    const random = [0, 0, 0, 0, 0, 0, 0x0c, 0xc3, 0x18, 0xc4, 0xdc, 0x0c, 0x0c, 0x07, 0x39, 0x8f];
    const id = uuidV7((b) => b.map((_, i) => random[i] ?? 0), at(0x017f22e279b0));
    expect(id).toBe('017f22e2-79b0-7cc3-98c4-dc0c0c07398f');
  });

  it.each([
    { name: 'all zero bytes', fill: 0x00, want: '00000000-0000-7000-8000-000000000000' },
    { name: 'all one bits', fill: 0xff, want: '00000000-0000-7fff-bfff-ffffffffffff' },
  ])('sets the version and variant bits over $name', ({ fill, want }) => {
    expect(uuidV7(bytes(fill), at(0))).toBe(want);
  });

  it('leads with the time, so later IDs sort after earlier ones', () => {
    const times = [0, 1, 255, 256, 1_700_000_000_000, 2 ** 48 - 1];
    const ids = times.map((t) => uuidV7(bytes(0xff), at(t)));
    expect([...ids].sort()).toEqual(ids);
    expect(ids.at(-1)?.startsWith('ffffffff-ffff-7')).toBe(true);
  });

  it.each([
    { name: 'a negative time', now: -1 },
    { name: 'a time past 48 bits', now: 2 ** 48 },
    { name: 'a fractional time', now: 1.5 },
  ])('refuses $name', ({ now }) => {
    expect(() => uuidV7(undefined, at(now))).toThrow(RangeError);
  });

  it('works with crypto.randomUUID removed, as in an insecure context', () => {
    const randomUUID = vi.spyOn(crypto, 'randomUUID').mockImplementation(() => {
      throw new TypeError('randomUUID is not available here');
    });
    expect(uuidV7()).toMatch(v7);
    expect(randomUUID).not.toHaveBeenCalled();
  });

  it('makes distinct IDs within one millisecond', () => {
    const ids = new Set(
      Array.from({ length: 200 }, () => uuidV7(undefined, at(1_700_000_000_000))),
    );
    expect(ids.size).toBe(200);
    for (const id of ids) {
      expect(id).toMatch(v7);
    }
  });

  it.each([
    { name: 'a request ID', make: requestId, prefix: 'r-' },
    { name: 'an idempotency key', make: idempotencyKey, prefix: 'k-' },
  ])('$name carries its prefix', ({ make, prefix }) => {
    const id = make();
    expect(id.startsWith(prefix)).toBe(true);
    expect(id.slice(2)).toMatch(v7);
  });

  it('refuses a random source that gives the wrong length', () => {
    expect(() => uuidV7(() => new Uint8Array(4))).toThrow(RangeError);
  });
});
