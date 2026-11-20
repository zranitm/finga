import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import { describe, expect, it } from 'vitest';

import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import type { Box } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { groundPixels, PX_PER_M } from './map-image';
import type { WorldMap } from './world3d/map';

/** A 16 m by 8 m map: flat in its west half, a 4 m cliff from x 8 to 12, a plateau east of it, and the given solids. */
function map(boxes: Box[] = []): WorldMap {
  const row = [0, 0, 0, 4, 4];
  return {
    id: 'm',
    ground: 'grass',
    sky: 'day',
    geometry: {
      size_x: 16,
      size_z: 8,
      terrain: terrain(4, 5, 3, [...row, ...row, ...row], movement.max_slope),
      boxes,
      cylinders: [{ x: 2, z: 6, radius: 1, top: 3 }],
    },
    solids: [],
    prepWall: null,
    props: [],
    portals: [],
    region: null,
  };
}

/** The pixel's red, green and blue at (x, z). */
function at(m: WorldMap, x: number, z: number): number[] {
  const p = groundPixels(m);
  const i =
    (Math.floor((m.geometry.size_z - z) * PX_PER_M) * p.width + Math.floor(x * PX_PER_M)) * 4;
  return [...p.data.slice(i, i + 3)];
}
const light = (rgb: number[]) => rgb.reduce((a, b) => a + b, 0);

describe('groundPixels', () => {
  it('draws 4 pixels a metre, north up, the high ground lighter and the steep ground darker', () => {
    const m = map();
    const p = groundPixels(m);
    expect([p.width, p.height]).toEqual([64, 32]);
    const low = light(at(m, 4, 3));
    const cliff = light(at(m, 10, 3));
    const high = light(at(m, 14, 3));
    expect(high).toBeGreaterThan(low);
    expect(cliff).toBeLessThan(low);
  });

  it('shades by height above the map’s lowest ground: the map raised 2 m draws the same', () => {
    const m = map();
    const t = m.geometry.terrain;
    const raised = {
      ...m,
      geometry: {
        ...m.geometry,
        terrain: terrain(
          t.cell,
          t.nx,
          t.nz,
          t.heights.map((h) => h + 2),
          movement.max_slope,
        ),
      },
    };
    expect(groundPixels(raised).data).toEqual(groundPixels(m).data);
  });

  it('draws a solid’s footprint dark: a cylinder’s disc in the north west, north up', () => {
    const m = map();
    expect(light(at(m, 2, 6))).toBeLessThan(light(at(m, 4, 3)) / 2);
    expect(light(at(m, 2, 2))).toBe(light(at(m, 4, 3))); // the same spot mirrored south: open ground
  });

  it('finds a point’s solids by its bucket, drawing what testing every solid draws', () => {
    const across = {
      x: 8,
      z: 4,
      half_w: 3,
      half_d: 1,
      ux: Math.cos(0.5),
      uz: -Math.sin(0.5),
      top: 2,
    }; // over the buckets' line at x = 8
    const m = map([across, { x: 13, z: 2, half_w: 1, half_d: 1, ux: 1, uz: 0, top: 1 }]);
    const bucketed = { ...m, geometry: indexed(m.geometry) };
    expect(bucketed.geometry.buckets?.nx).toBe(2);
    expect(groundPixels(bucketed).data).toEqual(groundPixels(m).data);
    expect(light(at(bucketed, 8, 4))).toBeLessThan(light(at(m, 4, 3)) / 2);
  });

  it('lays a turned box’s footprint where its 3D model stands', () => {
    const yaw = (30 * Math.PI) / 180;
    const box = {
      x: 6,
      z: 4,
      half_w: 2,
      half_d: 0.75,
      ux: Math.cos(yaw),
      uz: -Math.sin(yaw),
      top: 2,
    };
    const m = map([box]);
    const turn = Matrix.RotationY(Math.atan2(-box.uz, box.ux)); // as place.ts turns its model
    const corner = (u: number, v: number, k: number) => {
      const w = Vector3.TransformCoordinates(
        new Vector3(u * k * box.half_w, 0, v * k * box.half_d),
        turn,
      );
      return light(at(m, box.x + w.x, box.z + w.z));
    };
    const open = light(at(m, 4, 1));
    for (const [u, v] of [
      [1, 1],
      [1, -1],
      [-1, 1],
      [-1, -1],
    ] as const) {
      expect([u, v, corner(u, v, 0.85) < open / 2]).toEqual([u, v, true]); // just inside each corner
      expect([u, v, corner(u, v, 1.25) < open / 2]).toEqual([u, v, false]); // just past it
    }
  });
});
