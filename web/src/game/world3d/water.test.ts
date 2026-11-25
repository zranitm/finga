import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { describe, expect, it } from 'vitest';

import type { Lake, River } from './region-bake';
import { linearOf, Palette } from './toon';
import { drawWater, lakeArrays, riverArrays } from './water';
import type { WaterArrays } from './water';

type Corner = readonly [number, number];

/** Each triangle's corners, x and z. */
function triangles(a: WaterArrays): [Corner, Corner, Corner][] {
  const at = (i: number): Corner => [a.positions[3 * i]!, a.positions[3 * i + 2]!];
  const out: [Corner, Corner, Corner][] = [];
  for (let t = 0; t < a.indices.length; t += 3) {
    out.push([at(a.indices[t]!), at(a.indices[t + 1]!), at(a.indices[t + 2]!)]);
  }
  return out;
}

/** The y of the cross product of a triangle's first two sides: the ground's triangles, east then north, give -1 for a unit one. */
function turn([a, b, c]: [Corner, Corner, Corner]): number {
  const [ux, uz] = [b[0] - a[0], b[1] - a[1]];
  const [vx, vz] = [c[0] - a[0], c[1] - a[1]];
  return uz * vx - ux * vz;
}

/** Whether p lies in triangle t, edges included. */
function inside(p: Corner, t: [Corner, Corner, Corner]): boolean {
  const side = (a: Corner, b: Corner) =>
    (b[0] - a[0]) * (p[1] - a[1]) - (b[1] - a[1]) * (p[0] - a[0]);
  const s = [side(t[0], t[1]), side(t[1], t[2]), side(t[2], t[0])];
  return s.every((v) => v >= -1e-9) || s.every((v) => v <= 1e-9);
}

/** The area inside a polygon's corners. */
function area(c: readonly Corner[]): number {
  let twice = 0;
  c.forEach(([x0, z0], i) => {
    const [x1, z1] = c[(i + 1) % c.length]!;
    twice += x0 * z1 - x1 * z0;
  });
  return Math.abs(twice) / 2;
}

/** The distance from p to the line through a and b. */
function toLine(p: Corner, a: Corner, b: Corner): number {
  const [dx, dz] = [b[0] - a[0], b[1] - a[1]];
  return Math.abs((p[0] - a[0]) * dz - (p[1] - a[1]) * dx) / Math.hypot(dx, dz);
}

const TINY_RIVER: River = {
  path: [0, 2, 6, 12, 20, 30, 42, 56, 72, 86, 98, 108, 116, 122, 126, 128].map(
    (z) => [96, z] as const,
  ),
  width: 6,
  levels: [...Array<number>(9).fill(-1.079), ...Array<number>(7).fill(-1.165)],
};
const TEST_LAKE: Lake = {
  area: [
    [182, 40],
    [215, 46],
    [210, 80],
    [176, 74],
  ],
  level: 4.5,
}; // the test region's

describe('a region’s water', () => {
  it('lays a river along its path at each point’s level, its channel and 2 m of bank either side, facing up as the ground does', () => {
    const a = riverArrays(TINY_RIVER);
    expect(a.positions.length).toBe(3 * 2 * TINY_RIVER.path.length);
    TINY_RIVER.path.forEach(([, z], i) => {
      const [lx, ly, lz, rx, ry, rz] = a.positions.slice(6 * i, 6 * i + 6);
      expect([lx, lz, rx, rz]).toEqual([91, z, 101, z]); // 3 m of channel and 2 of bank each side of x = 96
      expect([ly, ry]).toEqual([
        Math.fround(TINY_RIVER.levels[i]!),
        Math.fround(TINY_RIVER.levels[i]!),
      ]);
    });
    const tris = triangles(a);
    expect(tris).toHaveLength(2 * (TINY_RIVER.path.length - 1));
    for (const t of tris) {
      expect(turn(t)).toBeLessThan(0);
    }
  });

  it('keeps a river round its bends: each point’s pair square across it, whichever way it runs', () => {
    const bend: River = {
      path: [
        [0, 0],
        [10, 0],
        [20, 10],
        [20, 20],
      ],
      width: 4,
      levels: [0, 0, 0, 0],
    };
    for (const r of [bend, { ...bend, path: bend.path.toReversed() }]) {
      const a = riverArrays(r);
      r.path.forEach(([x, z], i) => {
        const [lx, , lz, rx, , rz] = a.positions.slice(6 * i, 6 * i + 6);
        expect(Math.hypot(lx! - x, lz! - z)).toBeCloseTo(4, 5); // 2 m of channel, 2 of bank, in float32
        expect(Math.hypot(rx! - x, rz! - z)).toBeCloseTo(4, 5);
        expect([(lx! + rx!) / 2, (lz! + rz!) / 2]).toEqual([
          expect.closeTo(x, 5),
          expect.closeTo(z, 5),
        ]);
        const [px, pz] = r.path[Math.max(i - 1, 0)]!;
        const [nx, nz] = r.path[Math.min(i + 1, r.path.length - 1)]!;
        expect((lx! - rx!) * (nx - px) + (lz! - rz!) * (nz - pz)).toBeCloseTo(0, 4); // square to the line from the point before to the one after
      });
      for (const t of triangles(a)) {
        expect(turn(t)).toBeLessThan(0);
      }
    }
  });

  it('lays a lake over its area grown 2 m past every side, at its level, cut into triangles facing up, whichever way its corners run', () => {
    for (const lake of [TEST_LAKE, { ...TEST_LAKE, area: TEST_LAKE.area.toReversed() }]) {
      const a = lakeArrays(lake);
      const grown: Corner[] = [];
      for (let i = 0; i < a.positions.length; i += 3) {
        expect(a.positions[i + 1]).toBe(4.5);
        grown.push([a.positions[i]!, a.positions[i + 2]!]);
      }
      const tris = triangles(a);
      expect(tris).toHaveLength(lake.area.length - 2);
      expect(tris.reduce((s, t) => s + area(t), 0)).toBeCloseTo(area(grown), 6); // covered once over
      for (const t of tris) {
        expect(turn(t)).toBeLessThan(0);
      }
      lake.area.forEach((c, i) => {
        const g = grown[i]!;
        const [prev, next] = [lake.area.at(i - 1)!, lake.area[(i + 1) % lake.area.length]!];
        expect(toLine(g, prev, c)).toBeCloseTo(2, 4); // 2 m out from both its sides' lines
        expect(toLine(g, c, next)).toBeCloseTo(2, 4);
        expect(tris.some((t) => inside(c, t))).toBe(true);
      });
      expect(area(grown)).toBeGreaterThan(area(lake.area));
    }
  });

  it('caps a sharp lake corner at 6 m out, so it doesn’t spike', () => {
    const a = lakeArrays({
      area: [
        [0, 0],
        [40, 1],
        [0, 2],
      ],
      level: 0,
    });
    expect(Math.hypot(a.positions[3]! - 40, a.positions[5]! - 1)).toBeCloseTo(6, 5);
  });

  it('draws each river and lake as a mesh of its own under the region, toon-drawn see-through in the water’s colour, and lets them go with it', () => {
    const engine = new NullEngine();
    const scene = new Scene(engine);
    const root = new TransformNode('region', scene);
    const made = drawWater(
      scene,
      { water: 0x5b8fc7, rivers: [TINY_RIVER], lakes: [TEST_LAKE] },
      new Palette(scene),
      root,
    );
    expect(made.map((m) => m.name)).toEqual(['river 0', 'lake 0']);
    for (const m of made) {
      expect(m.parent).toBe(root);
      expect(m.material?.name).toBe(`toon - ${linearOf(0x5b8fc7).toHexString()} cut 0.85`); // the water's colour, linear
      expect(m.receiveShadows).toBe(true);
      expect(m.getVerticesData('normal')!.every((v, k) => v === (k % 3 === 1 ? 1 : 0))).toBe(true);
    }
    root.dispose();
    expect(scene.meshes).toEqual([]);
    scene.dispose();
    engine.dispose();
  });
});
