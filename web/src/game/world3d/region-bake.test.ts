import { afterEach, describe, expect, it, vi } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';
import { fixtureFile, HOLLOW, TINY as REF, tinyFile as bakeFile } from '@/testing/region-fixture';

import { loadRegion, readChunk, readLight, readOverview } from './region-bake';
import type { Chunk } from './region-bake';

const N = 33; // samples a chunk side: 64 m at 2 m, and the shared edge

/** Chunk c's height at sample (ix, iz) from its south-west corner, -1 and N reaching into its border. */
function h(c: Chunk, ix: number, iz: number): number {
  return c.heights[(iz + 1) * (c.n + 2) + ix + 1]!;
}

describe('a region bake, read as regionbake writes it', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads region.json and the overview from the folder its hash names', async () => {
    servePublic();
    const { region, overview } = await loadRegion(REF);
    expect(region).toMatchObject({
      ref: REF,
      sky: 'day',
      size: [128, 128],
      cell: 2,
      chunk: 64,
      chunks: [2, 2],
    });
    expect(region.palette).toContain(0x8cc265); // kit.yaml's grass
    expect(region.models).toContain('tree_oak');
    expect(region.areas).toEqual([{ id: 'field', name: 'Field', kind: 'hunting', levels: [1, 2] }]);
    expect(overview).toMatchObject({ nx: 17, nz: 17, spacing: 8 });
    expect(region.water).toBe(0x5b8fc7); // kit.yaml's water
    expect(region.rivers).toHaveLength(1);
    const [river] = region.rivers;
    expect(river!.width).toBe(6);
    expect(river!.path[0]).toEqual([96, 0]);
    expect(river!.path.at(-1)).toEqual([96, 128]);
    expect(river!.levels).toHaveLength(river!.path.length);
    expect(river!.levels[0]).toBeCloseTo(-1.079, 3);
    expect(region.lakes).toEqual([]);
  });

  it("holds one ground: a chunk's edge its neighbour's too, its border its neighbour's next samples in, the overview every 8 m of them", () => {
    const [c00, c10, c01] = ['0_0', '1_0', '0_1'].map((k) =>
      readChunk(bakeFile(`chunks/${k}.bin`)),
    );
    const o = readOverview(bakeFile('overview.bin'), 2);
    expect(c00).toMatchObject({ cx: 0, cz: 0, n: N });
    expect(c10).toMatchObject({ cx: 1, cz: 0, n: N });
    for (let k = -1; k <= N; k++) {
      expect(h(c00!, N - 1, k)).toBe(h(c10!, 0, k)); // the shared edge
      expect(h(c00!, N, k)).toBe(h(c10!, 1, k)); // the border is the neighbour's next sample in
      expect(h(c10!, -1, k)).toBe(h(c00!, N - 2, k));
      expect(h(c00!, k, N)).toBe(h(c01!, k, 1));
      expect(h(c00!, -1, k)).toBe(h(c00!, 0, k)); // held to the region at its edge
    }
    for (let iz = 0; iz <= 8; iz++) {
      for (let ix = 0; ix <= 8; ix++) {
        expect(o.heights[iz * o.nx + ix]).toBe(h(c00!, 4 * ix, 4 * iz));
        expect(o.colours[iz * o.nx + ix]).toBe(c00!.colours[4 * iz * N + 4 * ix]);
      }
    }
    expect(new Set(c00!.heights).size).toBeGreaterThan(100); // rolling ground, not a flat field
  });

  it('loads a cave’s flames and light map, lit under each flame and dark on the rock; the land has neither', async () => {
    servePublic();
    const land = await loadRegion(REF);
    expect([land.region.under, land.region.lights, land.light]).toEqual(['', [], null]);
    const { region, light } = await loadRegion(HOLLOW);
    expect([region.under, region.sky]).toEqual(['tiny', 'cave']);
    expect(region.lights.length).toBeGreaterThan(10);
    expect(new Set(region.lights.map((f) => f.group))).toEqual(new Set([0, 1]));
    expect(light).toMatchObject({ nx: 128, nz: 128 });
    const at = (x: number, z: number, group: number) =>
      light!.texels[2 * (Math.floor(z) * light!.nx + Math.floor(x)) + group]!;
    for (const f of region.lights) {
      expect(f.y).toBeGreaterThan(1); // its flame over the floor
      expect(at(f.x, f.z, f.group)).toBeGreaterThan(200);
    }
    expect([at(34, 110, 0), at(34, 110, 1)]).toEqual([0, 0]); // rock, 22 m past the hall's north wall
  });

  it('refuses a light map of other than two groups, or cut short', () => {
    const file = fixtureFile('hollow', 'light.bin');
    expect(readLight(file).texels).toHaveLength(2 * 128 * 128);
    const three = file.slice(0);
    new DataView(three).setUint16(10, 3, true);
    expect(() => readLight(three)).toThrow('light.bin: 3 groups');
    expect(() => readLight(file.slice(0, -1))).toThrow('light.bin: 2 groups');
  });

  it('holds each prop in the chunk holding its centre, of a model the region names', async () => {
    servePublic();
    const { region } = await loadRegion(REF);
    let count = 0;
    for (const k of ['0_0', '1_0', '0_1', '1_1']) {
      const c = readChunk(bakeFile(`chunks/${k}.bin`));
      c.props.models.forEach((m, i) => {
        const [x, z] = c.props.at.subarray(3 * i, 3 * i + 2);
        expect(x).toBeGreaterThanOrEqual(64 * c.cx);
        expect(x).toBeLessThanOrEqual(64 * (c.cx + 1));
        expect(z).toBeGreaterThanOrEqual(64 * c.cz);
        expect(z).toBeLessThanOrEqual(64 * (c.cz + 1));
        expect(region.models[m]).toBeDefined();
        count++;
      });
    }
    expect(count).toBeGreaterThan(20); // the tiny plan's oaks
  });

  it('holds each solid in the chunk holding its centre, at its index in the region’s lists, every index once', async () => {
    servePublic();
    const { region } = await loadRegion(REF);
    const seen = { cylinders: [] as number[], boxes: [] as number[] };
    for (const k of ['0_0', '1_0', '0_1', '1_1']) {
      const c = readChunk(bakeFile(`chunks/${k}.bin`));
      for (const [kind, per] of [
        ['cylinders', 4],
        ['boxes', 7],
      ] as const) {
        c[kind].index.forEach((i, j) => {
          const [x, z] = c[kind].at.subarray(per * j, per * j + 2);
          expect(Math.floor(x! / 64)).toBe(c.cx);
          expect(Math.floor(z! / 64)).toBe(c.cz);
          seen[kind].push(i);
        });
      }
    }
    for (const kind of ['cylinders', 'boxes'] as const) {
      expect(seen[kind].sort((a, b) => a - b)).toEqual(
        Array.from({ length: region.solids[kind] }, (_, i) => i),
      );
    }
    expect(region.solids).toEqual({ cylinders: 90, boxes: 2, reach: Math.hypot(3, 3.5) }); // the tiny plan's oaks and waypoint stone, and its two houses
  });

  it('marks the deep water: the river along x = 96, but for its ford and the road’s causeway', () => {
    const deepAt = (x: number, z: number) => {
      const c = readChunk(
        bakeFile(
          `chunks/${Math.min(Math.floor(x / 64), 1)}_${Math.min(Math.floor(z / 64), 1)}.bin`,
        ),
      );
      return c.barrier[((z - 64 * c.cz) / 2) * c.n + (x - 64 * c.cx) / 2] === 1;
    };
    expect(deepAt(96, 100)).toBe(true);
    expect(deepAt(96, 16)).toBe(true);
    expect(deepAt(96, 30)).toBe(false); // the ford
    expect(deepAt(96, 64)).toBe(false); // the road over it
    expect(deepAt(40, 40)).toBe(false);
  });

  it('refuses a chunk that ends early or runs on past its boxes', () => {
    const chunk = bakeFile('chunks/0_1.bin');
    expect(() => readChunk(chunk.slice(0, chunk.byteLength - 1))).toThrow(
      'chunk 0_1 ends 1 bytes early',
    );
    expect(() => readChunk(chunk.slice(0, 100))).toThrow(/chunk 0_1 ends \d+ bytes early/);
    const longer = new Uint8Array(chunk.byteLength + 4);
    longer.set(new Uint8Array(chunk));
    expect(() => readChunk(longer.buffer)).toThrow('chunk 0_1 runs 4 bytes past its end');
    expect(() => readChunk(chunk.slice(0, 8))).toThrow('not a format 3 FRCK: 8 bytes');
  });

  it('refuses a file of another format or kind, and says which bake failed to come', async () => {
    const chunk = bakeFile('chunks/0_0.bin');
    const old = chunk.slice(0);
    new DataView(old).setUint16(4, 1, true);
    expect(() => readChunk(old)).toThrow(/format 3 FRCK/);
    expect(() => readOverview(chunk, 2)).toThrow(/FROV/);
    vi.stubGlobal('fetch', (url: string) =>
      Promise.resolve(
        url.endsWith('.json')
          ? Response.json({
              ...(JSON.parse(new TextDecoder().decode(bakeFile('region.json'))) as object),
              format: 1,
            })
          : new Response(bakeFile('overview.bin')),
      ),
    );
    await expect(loadRegion(REF)).rejects.toThrow('bake format 1; this client reads 3');
    const json = JSON.parse(new TextDecoder().decode(bakeFile('region.json'))) as {
      palette: { name: string }[];
    };
    vi.stubGlobal('fetch', (url: string) =>
      Promise.resolve(
        url.endsWith('.json')
          ? Response.json({ ...json, palette: json.palette.filter((c) => c.name !== 'water') })
          : new Response(bakeFile('overview.bin')),
      ),
    );
    await expect(loadRegion(REF)).rejects.toThrow('region tiny: its palette has no water');
    const lake = {
      area: [
        [182, 40],
        [215, 46],
        [210, 80],
        [176, 74],
      ],
      level: 4.5,
    }; // the test region's
    vi.stubGlobal('fetch', (url: string) =>
      Promise.resolve(
        url.endsWith('.json')
          ? Response.json({ ...json, lakes: [lake], rivers: null }) // Go writes none as null
          : new Response(bakeFile('overview.bin')),
      ),
    );
    const { region } = await loadRegion(REF);
    expect([region.lakes, region.rivers]).toEqual([[lake], []]);
    vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
    await expect(loadRegion(REF)).rejects.toThrow(/regions\/tiny\/fixture\/.*: 404/);
  });
});
