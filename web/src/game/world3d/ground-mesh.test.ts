import { Color3 } from '@babylonjs/core/Maths/math.color';
import { describe, expect, it } from 'vitest';

import { tinyFile } from '@/testing/region-fixture';

import {
  chunkGround,
  DETAILS,
  linearPalette,
  overviewGround,
  overviewIndices,
} from './ground-mesh';
import type { GroundArrays } from './ground-mesh';
import { readChunk, readOverview } from './region-bake';
import type { Chunk } from './region-bake';

const PALETTE = [0x9c5d4b, 0xa88b62, 0x8cc265, 0xe2cf9a, 0xeef2f6, 0x6f6a63, 0x5b8fc7]; // the tiny bake's, from kit.yaml
const LINEAR = linearPalette(PALETTE);
const CELL = 2;
const N = 33;
const COARSEST = 4; // the overview's 8 m and the farthest detail's every 4th sample, both 4 samples at 2 m

const ground = (k: string): GroundArrays =>
  chunkGround(readChunk(tinyFile(`chunks/${k}.bin`)), CELL, LINEAR, COARSEST);

type V3 = [number, number, number];
const vertex = (a: Float32Array, i: number): V3 => [a[3 * i]!, a[3 * i + 1]!, a[3 * i + 2]!];

/** The right-handed cross of a triangle's edges: Babylon draws its front where its corners run clockwise, so this points away from whoever sees the front. */
function away(g: GroundArrays, tri: readonly number[]): V3 {
  const [a, b, c] = tri.map((i) => vertex(g.positions, i)) as [V3, V3, V3];
  const u = [b[0] - a[0], b[1] - a[1], b[2] - a[2]];
  const v = [c[0] - a[0], c[1] - a[1], c[2] - a[2]];
  return [
    u[1]! * v[2]! - u[2]! * v[1]!,
    u[2]! * v[0]! - u[0]! * v[2]!,
    u[0]! * v[1]! - u[1]! * v[0]!,
  ];
}

function triangles(indices: Uint32Array): number[][] {
  return Array.from({ length: indices.length / 3 }, (_, t) => [
    ...indices.subarray(3 * t, 3 * t + 3),
  ]);
}

describe("a region's ground as vertex arrays", () => {
  it('turns each palette colour linear as Babylon does', () => {
    PALETTE.forEach((rgb, i) => {
      const c = Color3.FromInts((rgb >> 16) & 0xff, (rgb >> 8) & 0xff, rgb & 0xff).toLinearSpace();
      expect([...LINEAR.subarray(3 * i, 3 * i + 3)]).toEqual([c.r, c.g, c.b].map(Math.fround));
    });
  });

  it("draws a chunk's samples cell apart, each in its colour, and three details that each cover it, every triangle facing up", () => {
    const c = readChunk(tinyFile('chunks/0_0.bin'));
    const g = ground('0_0');
    expect(g.positions.length).toBe(3 * (N * N + 4 * N));
    expect(vertex(g.positions, N + 2)).toEqual([4, c.heights[2 * (N + 2) + 3], 2]);
    const k = c.colours[N + 2]!;
    expect([...g.colors.subarray(4 * (N + 2), 4 * (N + 3))]).toEqual([
      ...LINEAR.subarray(3 * k, 3 * k + 3),
      1,
    ]);
    DETAILS.forEach((s, d) => {
      const tris = triangles(g.details[d]!);
      const flat = tris.filter((t) => t.every((i) => i < N * N));
      let area = 0;
      for (const t of flat) {
        const up = away(g, t);
        expect(up[1]).toBeLessThan(0); // its front seen from above
        area += -up[1] / 2;
      }
      expect(area).toBeCloseTo(64 * 64);
      expect(tris.length - flat.length).toBe(4 * 2 * ((N - 1) / s)); // a skirt quad along each edge every s samples
    });
  });

  it('hangs a skirt under every edge, facing out of the chunk, below where a coarser neighbour draws the edge', () => {
    const g = ground('0_0');
    const out: Record<number, V3> = { 0: [0, 0, -1], 1: [0, 0, 1], 2: [-1, 0, 0], 3: [1, 0, 0] }; // south, north, west, east
    for (const indices of g.details) {
      for (const t of triangles(indices).filter((tri) => tri.some((i) => i >= N * N))) {
        const foot = t.find((i) => i >= N * N)!;
        const o = out[Math.floor((foot - N * N) / N)]!;
        const a = away(g, t);
        expect(a[0] * o[0] + a[2] * o[2]).toBeLessThan(0); // its front seen from outside
      }
    }
    const y = (i: number) => g.positions[3 * i + 1]!;
    for (let e = 0; e < 4; e++) {
      const edge = (k: number) => [k, (N - 1) * N + k, k * N, k * N + N - 1][e]!;
      for (let k = 0; k + COARSEST < N; k += COARSEST) {
        for (let j = 0; j <= COARSEST; j++) {
          const line = y(edge(k)) + ((y(edge(k + COARSEST)) - y(edge(k))) * j) / COARSEST;
          const foot = N * N + e * N + k + j;
          expect(y(foot)).toBeLessThan(Math.min(line, y(edge(k + j))) - 0.49);
          expect(vertex(g.normals, foot)).toEqual(vertex(g.normals, edge(k + j)));
        }
      }
    }
  });

  it('gives a shared edge the same normals from both its chunks, so no toon band steps along the seam', () => {
    const [g00, g10, g01] = ['0_0', '1_0', '0_1'].map(ground) as [
      GroundArrays,
      GroundArrays,
      GroundArrays,
    ];
    for (let k = 0; k < N; k++) {
      expect(vertex(g00.normals, k * N + N - 1)).toEqual(vertex(g10.normals, k * N));
      expect(vertex(g00.normals, (N - 1) * N + k)).toEqual(vertex(g01.normals, k));
    }
    const lean = new Set(
      Array.from({ length: N * N }, (_, i) => g00.normals[3 * i + 1]!.toFixed(3)),
    );
    expect(lean.size).toBeGreaterThan(10); // the ground rolls
  });

  /** A chunk n samples a side whose bordered heights are h at each sample, the border one beyond its edges. */
  const madeUp = (n: number, h: (ix: number, iz: number) => number): Chunk => ({
    cx: 0,
    cz: 0,
    n,
    heights: Float32Array.from({ length: (n + 2) ** 2 }, (_, i) =>
      h((i % (n + 2)) - 1, Math.floor(i / (n + 2)) - 1),
    ),
    colours: new Uint8Array(n * n),
    props: readChunk(tinyFile('chunks/0_0.bin')).props,
  });

  it('leans each normal by the slope either side of its sample, its border’s too', () => {
    const g = chunkGround(
      madeUp(5, (ix) => ix * ix),
      CELL,
      LINEAR,
      COARSEST,
    ); // a bowl along x
    for (const ix of [0, 2, 4]) {
      const [x, y, z] = vertex(g.normals, 2 * 5 + ix);
      const slope = ((ix + 1) ** 2 - (ix - 1) ** 2) / (2 * CELL);
      expect(x / y).toBeCloseTo(-slope, 6);
      expect(z).toBeCloseTo(0, 9);
    }
  });

  it('hangs a skirt just SKIRT_PAD below an edge its coarse neighbour draws straight', () => {
    const g = chunkGround(
      madeUp(9, (ix, iz) => 0.3 * ix + 0.2 * iz),
      CELL,
      LINEAR,
      COARSEST,
    ); // a tilted plane: every edge a straight line
    const y = (i: number) => g.positions[3 * i + 1]!;
    for (let k = 0; k < 9; k++) {
      expect(y(9 * 9 + k)).toBeCloseTo(y(k) - 0.5, 5); // the south edge's feet
    }
  });

  it('draws the overview in the world every 8 m, with holes under the chunks drawn', () => {
    const o = readOverview(tinyFile('overview.bin'), CELL);
    const g = overviewGround(o, LINEAR);
    expect(vertex(g.positions, o.nx + 3)).toEqual([24, o.heights[o.nx + 3], 8]);
    const whole = overviewIndices(o, 8, () => false);
    expect(whole.length).toBe(16 * 16 * 6);
    for (const t of triangles(whole)) {
      expect(away({ ...g, details: [] }, t)[1]).toBeLessThan(0);
    }
    expect(overviewIndices(o, 8, (cx, cz) => cx === 0 && cz === 0).length).toBe(
      (16 * 16 - 8 * 8) * 6,
    );
    expect(overviewIndices(o, 8, () => true).length).toBe(0);
    const left = triangles(overviewIndices(o, 8, (cx, cz) => cx === 1 || cz === 1));
    expect(left.every((t) => t.every((i) => i % o.nx <= 8 && Math.floor(i / o.nx) <= 8))).toBe(
      true,
    );
  });
});
