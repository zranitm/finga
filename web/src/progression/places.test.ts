// A place below the test region, and a region beside it, which no shipped region has yet: the maps fixed here, as places.ts reads them as it loads.
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';

import type * as PlacesModule from './places';

vi.mock('@/gamedata/maps', async (importOriginal) => {
  const { belowTheTestRegion } = await import('@/testing/places-below');
  return belowTheTestRegion(await importOriginal());
});

// A worker shares its modules across files, and one before this may have loaded the real maps: this file loads places.ts afresh, under the mock, and leaves the next file to load its own.
let places: typeof PlacesModule;
beforeAll(async () => {
  vi.resetModules();
  places = await import('./places');
});
afterAll(() => {
  vi.resetModules();
});

describe('the places below a region (underground-and-underwater.md section 7)', () => {
  it('are the regions whose region above it is', () => {
    expect(places.placesBelow('test_region').map((m) => m.id)).toEqual(['deep']);
    expect(places.placesBelow('deep')).toEqual([]);
    expect(places.placesBelow('next_region')).toEqual([]);
  });

  it('span their hunting areas’ levels, a town’s aside', () => {
    const deep = places.placesBelow('test_region')[0]!;
    expect(places.levelsOf(deep)).toEqual([3, 7]);
    expect(places.levelsOf({ areas: deep.areas.filter((a) => a.levels === null) })).toBeNull();
  });

  it('are reached by the gate between, down from the region and up from the place, and no other pair has one', () => {
    expect(places.gateBetween('test_region', 'deep')).toEqual({ x: 60, z: 40 }); // the cave mouth
    expect(places.gateBetween('deep', 'test_region')).toEqual({ x: 20, z: 64 }); // the way back
    expect(places.gateBetween('test_region', 'next_region')).toBeNull(); // a gate, but beside it
    expect(places.gateBetween('next_region', 'deep')).toBeNull();
    expect(places.gateBetween('test_region', 'nowhere')).toBeNull();
  });
});
