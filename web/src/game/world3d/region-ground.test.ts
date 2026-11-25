import { afterEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import { terrain } from '@/sim/terrain';
import { replay, vectors } from '@/testing/movement-vectors';
import { servePublic } from '@/testing/public-on-disk';
import { fixtureChunks, fixtureFile, TINY, tinyFile } from '@/testing/region-fixture';

import { loadRegion, readChunk } from './region-bake';
import type { Region } from './region-bake';
import { RegionGround } from './region-ground';

const KEYS = ['0_0', '1_0', '0_1', '1_1'] as const;

async function tiny(): Promise<Region> {
  servePublic();
  return (await loadRegion(TINY)).region;
}

/** The tiny region's ground with the chunks keys names added, in that order. */
async function ground(keys: readonly string[] = KEYS): Promise<RegionGround> {
  const g = new RegionGround(await tiny(), movement.max_slope);
  for (const k of keys) {
    g.add(readChunk(tinyFile(`chunks/${k}.bin`)));
  }
  return g;
}

describe("a region's ground, built from its chunks", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lays each chunk’s samples and deep water at their place in one grid, its steep marks as a whole grid marks them', async () => {
    const g = await ground();
    const t = g.geometry.terrain;
    expect([t.cell, t.nx, t.nz]).toEqual([2, 65, 65]);
    for (const k of KEYS) {
      const c = readChunk(tinyFile(`chunks/${k}.bin`));
      for (let iz = 0; iz < c.n; iz++) {
        for (let ix = 0; ix < c.n; ix++) {
          const s = (c.cz * 32 + iz) * 65 + c.cx * 32 + ix;
          expect(t.heights[s]).toBe(c.heights[(iz + 1) * (c.n + 2) + ix + 1]);
          expect(t.barrier![s]).toBe(c.barrier[iz * c.n + ix]);
        }
      }
    }
    const whole = terrain(2, 65, 65, t.heights, movement.max_slope);
    expect(t.steepX).toEqual(whole.steepX);
    expect(t.steepZ).toEqual(whole.steepZ);
    expect(whole.steepX.some((s) => s === 1)).toBe(true); // the river's banks
    expect(Array.from(t.barrier!).filter((d) => d === 1).length).toBeGreaterThan(50);
  });

  it('holds each solid as its chunk records it, at its index', async () => {
    const g = await ground();
    for (const k of KEYS) {
      const c = readChunk(tinyFile(`chunks/${k}.bin`));
      c.cylinders.index.forEach((i, j) => {
        const [x, z, radius, top] = c.cylinders.at.subarray(4 * j, 4 * j + 4);
        expect(g.geometry.cylinders[i]).toEqual({ x, z, radius, top });
      });
      c.boxes.index.forEach((i, j) => {
        const [x, z, half_w, half_d, ux, uz, top] = c.boxes.at.subarray(7 * j, 7 * j + 7);
        expect(g.geometry.boxes[i]).toEqual({ x, z, half_w, half_d, ux, uz, top });
      });
    }
  });

  it('holds every solid at its index once all chunks are in, bucketed as indexing them all at once buckets them, whatever order the chunks come in', async () => {
    const region = await tiny();
    for (const order of [KEYS, ['1_1', '0_1', '1_0', '0_0']]) {
      const g = await ground(order);
      const { cylinders, boxes } = g.geometry;
      expect(cylinders).toHaveLength(region.solids.cylinders);
      expect(boxes).toHaveLength(region.solids.boxes);
      expect(Object.keys(cylinders)).toHaveLength(region.solids.cylinders); // no index left unfilled
      expect(Object.keys(boxes)).toHaveLength(region.solids.boxes);
      expect(g.geometry.buckets).toEqual(
        indexed({ size_x: 128, size_z: 128, terrain: g.geometry.terrain, boxes, cylinders })
          .buckets,
      );
    }
  });

  it('files a box over a chunk’s edge in the buckets on both sides, though only the chunk holding its centre carries it', async () => {
    const g = await ground(['0_1']);
    const s = g.geometry.buckets!;
    const house = g.geometry.boxes.findIndex(
      (b) => b !== undefined && b.x < 64 && b.x + b.half_w > 64,
    );
    expect(house).toBeGreaterThanOrEqual(0);
    const at = (x: number, z: number) => s.boxes[Math.floor(z / 8) * s.nx + Math.floor(x / 8)];
    expect(at(62, 83)).toContain(house);
    expect(at(65, 83)).toContain(house); // in chunk 1_1, not yet in
  });

  it('covers a point once every chunk within reach of it is in, and a chunk added again changes nothing', async () => {
    const g = await ground(['0_0']);
    expect(g.covers(20, 20, 16)).toBe(true);
    expect(g.covers(56, 20, 16)).toBe(false); // 8 m from chunk 1_0
    expect(g.covers(20, 56, 4)).toBe(true);
    const before = Float32Array.from(g.geometry.terrain.heights);
    g.add(readChunk(tinyFile('chunks/0_0.bin')));
    expect(g.geometry.terrain.heights).toEqual(before);
    g.add(readChunk(tinyFile('chunks/1_0.bin')));
    expect(g.covers(56, 20, 16)).toBe(true);
    expect(g.covers(127, 127, 0)).toBe(false);
  });

  it('refuses a chunk that doesn’t fit the region, changing nothing', async () => {
    const g = await ground(['0_0']);
    const t = g.geometry.terrain;
    const before = [Float32Array.from(t.heights), Uint8Array.from(t.steepX)];
    const c = readChunk(tinyFile('chunks/1_0.bin'));
    const own = c.cylinders.index[0]!;
    const theirs = readChunk(tinyFile('chunks/0_0.bin')).cylinders.index[0]!;
    const solids = (index: number[]) => ({
      index: Uint32Array.from(index),
      at: new Float32Array(4 * index.length),
    });
    for (const [bad, refusal] of [
      [{ ...c, cx: 2 }, "chunk 2_0 is outside the region's 2 by 2"],
      [{ ...c, cz: 2 }, "chunk 1_2 is outside the region's 2 by 2"],
      [{ ...c, n: 17 }, "chunk 1_0 has 17 samples a side; the region's have 33"],
      [
        { ...c, cylinders: solids([90]) },
        "chunk 1_0's cylinder 90 is past the region's 90, or held twice",
      ],
      [
        { ...c, cylinders: solids([own, own]) },
        `chunk 1_0's cylinder ${own} is past the region's 90, or held twice`,
      ],
      [
        { ...c, cylinders: solids([theirs]) },
        `chunk 1_0's cylinder ${theirs} is past the region's 90, or held twice`,
      ],
      [
        { ...c, boxes: { index: Uint32Array.of(2), at: new Float32Array(7) } },
        "chunk 1_0's box 2 is past the region's 2, or held twice",
      ],
    ] as const) {
      expect(() => g.add(bad), refusal).toThrow(refusal);
    }
    expect([Float32Array.from(t.heights), Uint8Array.from(t.steepX)]).toEqual(before);
    expect(g.covers(96, 20, 0)).toBe(false);
    g.add(c);
    expect(g.covers(96, 20, 0)).toBe(true);
  });
});

describe('the movement vectors on a bake', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const baked = vectors.filter((v) => v.region);
  it('walk both test plans', () => {
    expect(new Set(baked.map((v) => v.region))).toEqual(new Set(['tiny', 'steep']));
  });

  for (const v of baked) {
    it(`${v.name} replays to the bit on the ground its bake's chunks build`, async () => {
      servePublic();
      const { region } = await loadRegion({ id: v.region!, hash: 'fixture' });
      const g = new RegionGround(region, v.params.max_slope);
      for (const file of fixtureChunks(v.region!)) {
        g.add(readChunk(fixtureFile(v.region!, file)));
      }
      const got = replay(v, g.geometry);
      expect(got).toHaveLength(v.expected.length);
      got.forEach((s, i) => {
        expect(s, `sub-step ${i + 1}`).toEqual(v.expected[i]!);
      });
    });
  }
});
