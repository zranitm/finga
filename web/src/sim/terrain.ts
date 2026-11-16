// Mirrors server/internal/shared/sim/terrain.go op for op: the ground's height grid and each cell's steep marks.

/** The ground's height grid, `cell` metres apart, with each cell's steep marks; `terrain` builds it. */
export interface Terrain {
  readonly cell: number;
  readonly nx: number; // samples along x
  readonly nz: number; // samples along z
  readonly heights: readonly number[]; // row by row from z = 0, each row from x = 0
  readonly steepX: readonly boolean[]; // per cell: a rise across either of its x edges exceeds max_slope × cell
  readonly steepZ: readonly boolean[]; // likewise across its z edges
}

/** The grid with its steep marks for maxSlope, a rise per metre. */
export function terrain(
  cell: number,
  nx: number,
  nz: number,
  heights: readonly number[],
  maxSlope: number,
): Terrain {
  const limit = maxSlope * cell;
  const steepX: boolean[] = [];
  const steepZ: boolean[] = [];
  for (let iz = 0; iz < nz - 1; iz++) {
    for (let ix = 0; ix < nx - 1; ix++) {
      const h00 = heights[iz * nx + ix]!;
      const h10 = heights[iz * nx + ix + 1]!;
      const h01 = heights[(iz + 1) * nx + ix]!;
      const h11 = heights[(iz + 1) * nx + ix + 1]!;
      steepX.push(Math.abs(h10 - h00) > limit || Math.abs(h11 - h01) > limit);
      steepZ.push(Math.abs(h01 - h00) > limit || Math.abs(h11 - h10) > limit);
    }
  }
  return { cell, nx, nz, heights, steepX, steepZ };
}

/** The cell holding coordinate v along an axis of n samples, and how far into it v lies, 0 to 1. */
function at(t: Terrain, v: number, n: number): [number, number] {
  const f = v / t.cell;
  const i = Math.min(Math.max(Math.floor(f), 0), n - 2); // Math.max turns a -0 floor into +0, as Go's int() has no -0
  return [i, f - i];
}

/** The bilinear height at (x, z). */
export function height(t: Terrain, x: number, z: number): number {
  const [ix, fx] = at(t, x, t.nx);
  const [iz, fz] = at(t, z, t.nz);
  const h00 = t.heights[iz * t.nx + ix]!;
  const h10 = t.heights[iz * t.nx + ix + 1]!;
  const h01 = t.heights[(iz + 1) * t.nx + ix]!;
  const h11 = t.heights[(iz + 1) * t.nx + ix + 1]!;
  const near = h00 + (h10 - h00) * fx;
  const far = h01 + (h11 - h01) * fx;
  return near + (far - near) * fz;
}

/** The highest of the four samples of the cell holding (x, z). */
export function top(t: Terrain, x: number, z: number): number {
  const [ix] = at(t, x, t.nx);
  const [iz] = at(t, z, t.nz);
  const i = iz * t.nx + ix;
  return Math.max(t.heights[i]!, t.heights[i + 1]!, t.heights[i + t.nx]!, t.heights[i + t.nx + 1]!);
}

/** Whether the cell holding (x, z) is steep along x, or along z when alongZ. */
export function steep(t: Terrain, x: number, z: number, alongZ: boolean): boolean {
  const [ix] = at(t, x, t.nx);
  const [iz] = at(t, z, t.nz);
  return (alongZ ? t.steepZ : t.steepX)[iz * (t.nx - 1) + ix]!;
}
