import { afterEach, describe, expect, it, vi } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';
import { TINY as REF, tinyFile as bakeFile } from '@/testing/region-fixture';

import { loadRegion, readChunk, readOverview } from './region-bake';
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
    expect(overview).toMatchObject({ nx: 17, nz: 17, spacing: 8 });
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

  it('refuses a file of another format or kind, and says which bake failed to come', async () => {
    const chunk = bakeFile('chunks/0_0.bin');
    const old = chunk.slice(0);
    new DataView(old).setUint16(4, 1, true);
    expect(() => readChunk(old)).toThrow(/format 2 FRCK/);
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
    await expect(loadRegion(REF)).rejects.toThrow('bake format 1; this client reads 2');
    vi.stubGlobal('fetch', () => Promise.resolve(new Response(null, { status: 404 })));
    await expect(loadRegion(REF)).rejects.toThrow(/regions\/tiny\/fixture\/.*: 404/);
  });
});
