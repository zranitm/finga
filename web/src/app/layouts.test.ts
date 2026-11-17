import { afterEach, describe, expect, it, vi } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';

import { layoutOf } from './layouts';

/** fetch as the public folder answers it, counting each map's requests; the first `failing` requests answer 500. */
function counted(failing = 0) {
  servePublic();
  const real = globalThis.fetch;
  const fetches = vi.fn((url: string) =>
    fetches.mock.calls.length <= failing
      ? Promise.resolve(new Response(null, { status: 500 }))
      : real(url),
  );
  vi.stubGlobal('fetch', fetches);
  return (id: string) => fetches.mock.calls.filter(([u]) => u.endsWith(`maps/${id}.json`)).length;
}

describe('layoutOf', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('gives a map’s size from its JSON, fetched once however often it’s asked for', async () => {
    const fetched = counted();
    const [a, b] = await Promise.all([layoutOf('cave'), layoutOf('cave')]);
    expect(await layoutOf('cave')).toBe(a);
    expect(b).toBe(a);
    expect(a.size).toEqual([expect.any(Number), expect.any(Number)]);
    expect(a.size[0]).toBeGreaterThan(0);
    expect(fetched('cave')).toBe(1);
  });

  it('fetches a map again after a fetch that failed', async () => {
    const fetched = counted(1);
    await expect(layoutOf('dune_road')).rejects.toThrow('map dune_road: 500');
    expect((await layoutOf('dune_road')).size[0]).toBeGreaterThan(0);
    expect(fetched('dune_road')).toBe(2);
  });
});
