// A map's ground seen from above, north up (map-views.md section 4): drawn once as the map loads, for the minimap and the map window to share; a region's from its bake's overview.
import { near } from '@/sim/solids';
import { height, steep } from '@/sim/terrain';

import { CLIFF, groundOf } from './world3d/grounds';
import type { RegionMap, WorldMap } from './world3d/map';

/** The image's pixels a metre. */
export const PX_PER_M = 4;

/** A region's image's: its overview has a sample every 8 m, which finer pixels would only blur. */
export const REGION_PX_PER_M = 1;

const LOW = 0.8; // the lowest ground's share of its colour, in linear light, rising to HIGH at the highest
const HIGH = 1.15;
const SOLID = 0.06; // a solid's footprint: this share of the ground's colour

/** An image's RGBA bytes, rows from the map's north edge down, columns from its west edge. */
export interface Pixels {
  readonly width: number;
  readonly height: number;
  readonly data: Uint8ClampedArray<ArrayBuffer>;
}

const toLinear = (c: number) => {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};
const toSrgb = (v: number) =>
  255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * Math.min(1, v) ** (1 / 2.4) - 0.055);

/** Whether (x, z) is inside one of the map's solids near it: a box along its axis, or a cylinder's disc. */
function inSolid(map: WorldMap, x: number, z: number): boolean {
  const g = map.geometry;
  for (const i of near(g, false, x, z, x, z)) {
    const b = g.boxes[i]!;
    const dx = x - b.x;
    const dz = z - b.z;
    if (
      Math.abs(dx * b.ux + dz * b.uz) <= b.half_w &&
      Math.abs(dz * b.ux - dx * b.uz) <= b.half_d
    ) {
      return true;
    }
  }
  return near(g, true, x, z, x, z).some((i) => {
    const c = g.cylinders[i]!;
    return (x - c.x) ** 2 + (z - c.z) ** 2 <= c.radius ** 2;
  });
}

/** The ground's colour shaded lighter with height across the map, steep ground darker as the 3D ground draws it, and each solid's footprint dark; a region's is its overview's. */
export function groundPixels(map: WorldMap): Pixels {
  if (map.region) {
    return regionPixels(map.region);
  }
  const g = map.geometry;
  const t = g.terrain;
  const width = Math.round(g.size_x * PX_PER_M);
  const rows = Math.round(g.size_z * PX_PER_M);
  let low = Infinity;
  let high = -Infinity;
  for (const h of t.heights) {
    low = Math.min(low, h);
    high = Math.max(high, h);
  }
  const span = high - low;
  const base = groundOf(map.ground);
  const rgb = [(base >> 16) & 0xff, (base >> 8) & 0xff, base & 0xff].map(toLinear);
  const data = new Uint8ClampedArray(width * rows * 4);
  for (let r = 0; r < rows; r++) {
    const z = g.size_z - (r + 0.5) / PX_PER_M;
    for (let c = 0; c < width; c++) {
      const x = (c + 0.5) / PX_PER_M;
      let k: number;
      if (inSolid(map, x, z)) {
        k = SOLID;
      } else {
        k = span > 0 ? LOW + ((HIGH - LOW) * (height(t, x, z) - low)) / span : 1;
        if (steep(t, x, z, false) || steep(t, x, z, true)) {
          k *= CLIFF;
        }
      }
      const i = (r * width + c) * 4;
      data[i] = toSrgb(rgb[0]! * k);
      data[i + 1] = toSrgb(rgb[1]! * k);
      data[i + 2] = toSrgb(rgb[2]! * k);
      data[i + 3] = 255;
    }
  }
  return { width, height: rows, data };
}

/** A region's overview blended between its samples, its ground's colours shaded lighter with height across the region; its colours mark steep ground already. */
function regionPixels({ region, overview: o }: RegionMap): Pixels {
  const [sx, sz] = region.size;
  const width = Math.round(sx * REGION_PX_PER_M);
  const rows = Math.round(sz * REGION_PX_PER_M);
  const [low, high] = region.heights;
  const span = high - low;
  const linear = region.palette.map((c) =>
    [(c >> 16) & 0xff, (c >> 8) & 0xff, c & 0xff].map(toLinear),
  );
  const data = new Uint8ClampedArray(width * rows * 4);
  for (let r = 0; r < rows; r++) {
    const fz = Math.min((sz - (r + 0.5) / REGION_PX_PER_M) / o.spacing, o.nz - 1);
    const iz = Math.min(Math.floor(fz), o.nz - 2);
    const tz = fz - iz;
    for (let c = 0; c < width; c++) {
      const fx = Math.min((c + 0.5) / REGION_PX_PER_M / o.spacing, o.nx - 1);
      const ix = Math.min(Math.floor(fx), o.nx - 2);
      const tx = fx - ix;
      let h = 0;
      const rgb = [0, 0, 0];
      for (const [s, w] of [
        [iz * o.nx + ix, (1 - tx) * (1 - tz)],
        [iz * o.nx + ix + 1, tx * (1 - tz)],
        [(iz + 1) * o.nx + ix, (1 - tx) * tz],
        [(iz + 1) * o.nx + ix + 1, tx * tz],
      ] as const) {
        h += w * o.heights[s]!;
        const col = linear[o.colours[s]!]!;
        for (let k = 0; k < 3; k++) {
          rgb[k]! += w * col[k]!;
        }
      }
      const shade = span > 0 ? LOW + ((HIGH - LOW) * (h - low)) / span : 1;
      const i = (r * width + c) * 4;
      data[i] = toSrgb(rgb[0]! * shade);
      data[i + 1] = toSrgb(rgb[1]! * shade);
      data[i + 2] = toSrgb(rgb[2]! * shade);
      data[i + 3] = 255;
    }
  }
  return { width, height: rows, data };
}

/** The ground as an image's URL, or '' on a page that can't draw one. */
export function groundImage(map: WorldMap): string {
  if (typeof ImageData === 'undefined') {
    return '';
  }
  const p = groundPixels(map);
  const canvas = document.createElement('canvas');
  canvas.width = p.width;
  canvas.height = p.height;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    return '';
  }
  ctx.putImageData(new ImageData(p.data, p.width, p.height), 0, 0);
  return canvas.toDataURL();
}
