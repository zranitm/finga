// A map's ground seen from above, north up (map-views.md section 4): drawn once as the map loads, for the minimap and the map window to share; a region's is its bake's tiles.
import { near } from '@/sim/solids';
import { height, steep } from '@/sim/terrain';
import type { MapGround } from '@/state/map-view';

import { CLIFF, groundOf } from './world3d/grounds';
import type { WorldMap } from './world3d/map';
import { bakeUrl } from './world3d/region-bake';

/** The image's pixels a metre. */
export const PX_PER_M = 4;

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

/** A map's ground's colour shaded lighter with height across the map, steep ground darker as the 3D ground draws it, and each solid's footprint dark. */
export function groundPixels(map: WorldMap): Pixels {
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

/** The ground as the map views draw it: a region's baked tiles, or a map's image. */
export function mapGround(map: WorldMap): MapGround {
  return map.region
    ? { kind: 'tiles', folder: bakeUrl(map.region.ref, 'tiles') }
    : { kind: 'image', url: groundImage(map) };
}

/** A map's ground as an image's URL, or '' on a page that can't draw one. */
function groundImage(map: WorldMap): string {
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
