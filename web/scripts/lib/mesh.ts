// Shapes for the pieces we model in code (the wings, the face pieces): triangles in a KayKit-style atlas's swatches, built into one flat-shaded mesh a material.
import type { Buffer, Document, Mesh, Material } from '@gltf-transform/core';

export type P = readonly [number, number];
export type V = readonly [number, number, number];

/** One triangle with the swatch it's drawn in ("column,row" of the atlas's 8 × 4 grid), and the way its front faces: a flat piece's the back, a solid's away from its middle. */
export interface Tri {
  readonly v: readonly [V, V, V];
  readonly cell: string;
  readonly out: V;
}

export const COLUMNS = 8;
export const ROWS = 4;
export const ATLAS_PX = 128;

/** What a flat piece faces: the back, -z, where the camera is. */
export const BACK: V = [0, 0, -1];

export const sub = (a: V, b: V): V => [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
export const cross = (a: V, b: V): V => [
  a[1] * b[2] - a[2] * b[1],
  a[2] * b[0] - a[0] * b[2],
  a[0] * b[1] - a[1] * b[0],
];
export const dot = (a: V, b: V): number => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
export const lerp = (a: P, b: P, t: number): P => [
  a[0] + (b[0] - a[0]) * t,
  a[1] + (b[1] - a[1]) * t,
];

/** A triangle wound counter-clockwise seen from where its `out` points. */
export function faced(t: Tri): Tri {
  const [a, b, c] = t.v;
  return dot(cross(sub(b, a), sub(c, a)), t.out) < 0 ? { ...t, v: [a, c, b] } : t;
}

/** A flat triangle at depth z, its front toward the back. */
export function flat(a: P, b: P, c: P, z: number, cell: string): Tri {
  return faced({
    v: [
      [a[0], a[1], z],
      [b[0], b[1], z],
      [c[0], c[1], z],
    ],
    cell,
    out: BACK,
  });
}

/** A convex polygon as a fan from its first point. */
export function fan(points: readonly P[], z: number, cell: string): Tri[] {
  return points.slice(2).map((p, i) => flat(points[0]!, points[i + 1]!, p, z, cell));
}

/** A strip of width w from p to q, as two triangles. */
export function bar(p: P, q: P, w: number, z: number, cell: string): Tri[] {
  const len = Math.hypot(q[0] - p[0], q[1] - p[1]);
  const n: P = [(-(q[1] - p[1]) / len) * (w / 2), ((q[0] - p[0]) / len) * (w / 2)];
  const a: P = [p[0] + n[0], p[1] + n[1]];
  const b: P = [p[0] - n[0], p[1] - n[1]];
  const c: P = [q[0] - n[0], q[1] - n[1]];
  const d: P = [q[0] + n[0], q[1] + n[1]];
  return [flat(a, b, c, z, cell), flat(a, c, d, z, cell)];
}

const plus = (a: V, b: V): V => [a[0] + b[0], a[1] + b[1], a[2] + b[2]];
const scaled = (a: V, k: number): V => [a[0] * k, a[1] * k, a[2] * k];
const unit = (a: V): V => scaled(a, 1 / (Math.hypot(...a) || 1));

/** A quadrilateral a, b, c, d in order, as two triangles facing out. */
export function quad(a: V, b: V, c: V, d: V, out: V, cell: string): Tri[] {
  return [faced({ v: [a, b, c], cell, out }), faced({ v: [a, c, d], cell, out })];
}

/** A solid bar of square section w a side from p to q, its ends closed. */
export function beam(p: V, q: V, w: number, cell: string): Tri[] {
  const d = unit(sub(q, p));
  const n1 = unit(cross(d, Math.abs(d[1]) > 0.9 ? [1, 0, 0] : [0, 1, 0]));
  const n2 = cross(d, n1);
  const corner = (at: V, i: number): V =>
    plus(at, plus(scaled(n1, (i & 1 ? 1 : -1) * (w / 2)), scaled(n2, (i & 2 ? 1 : -1) * (w / 2))));
  const [p0, p1, p2, p3] = [0, 1, 3, 2].map((i) => corner(p, i)) as [V, V, V, V];
  const [q0, q1, q2, q3] = [0, 1, 3, 2].map((i) => corner(q, i)) as [V, V, V, V];
  return [
    ...quad(p0, p1, p2, p3, scaled(d, -1), cell),
    ...quad(q0, q1, q2, q3, d, cell),
    ...quad(p0, p1, q1, q0, scaled(n2, -1), cell),
    ...quad(p1, p2, q2, q1, n1, cell),
    ...quad(p2, p3, q3, q2, n2, cell),
    ...quad(p3, p0, q0, q3, scaled(n1, -1), cell),
  ];
}

/** A beam along each leg of a polyline. */
export function path(points: readonly V[], w: number, cell: string): Tri[] {
  return points.slice(1).flatMap((q, i) => beam(points[i]!, q, w, cell));
}

/** A round frame facing +z: an n-sided ring from radius inner to outer about c, solid from z0 to z1. */
export function ring(
  c: P,
  outer: number,
  inner: number,
  n: number,
  z0: number,
  z1: number,
  cell: string,
): Tri[] {
  const at = (r: number, i: number, z: number): V => [
    c[0] + r * Math.cos((2 * Math.PI * i) / n),
    c[1] + r * Math.sin((2 * Math.PI * i) / n),
    z,
  ];
  const tris: Tri[] = [];
  for (let i = 0; i < n; i++) {
    const mid = (2 * Math.PI * (i + 0.5)) / n;
    const radial: V = [Math.cos(mid), Math.sin(mid), 0];
    const [a, b] = [i, i + 1];
    tris.push(
      ...quad(at(outer, a, z0), at(outer, b, z0), at(outer, b, z1), at(outer, a, z1), radial, cell),
      ...quad(
        at(inner, a, z0),
        at(inner, b, z0),
        at(inner, b, z1),
        at(inner, a, z1),
        scaled(radial, -1),
        cell,
      ),
      ...quad(
        at(inner, a, z1),
        at(inner, b, z1),
        at(outer, b, z1),
        at(outer, a, z1),
        [0, 0, 1],
        cell,
      ),
      ...quad(
        at(inner, a, z0),
        at(inner, b, z0),
        at(outer, b, z0),
        at(outer, a, z0),
        [0, 0, -1],
        cell,
      ),
    );
  }
  return tris;
}

/** A round plate facing +z: an n-sided disc of radius r about c at depth z. */
export function disc(c: P, r: number, n: number, z: number, cell: string): Tri[] {
  const pts: P[] = Array.from({ length: n }, (_, i) => [
    c[0] + r * Math.cos((2 * Math.PI * i) / n),
    c[1] + r * Math.sin((2 * Math.PI * i) / n),
  ]);
  return fan(pts, z, cell).map((t) => faced({ ...t, out: [0, 0, 1] }));
}

/** A sheet over a grid of points, rows[i][j], its front the side away from `middle`. */
export function sheet(rows: readonly (readonly V[])[], middle: V, cell: string): Tri[] {
  return rows.slice(1).flatMap((row, i) =>
    row.slice(1).flatMap((_, j) => {
      const [a, b, c, d] = [rows[i]![j]!, rows[i]![j + 1]!, row[j + 1]!, row[j]!];
      return quad(a, b, c, d, sub(scaled(plus(plus(a, b), plus(c, d)), 0.25), middle), cell);
    }),
  );
}

/** The tris mirrored across the plane x = 0, each facing as it did. */
export function mirrored(tris: readonly Tri[]): Tri[] {
  const flip = (v: V): V => [-v[0], v[1], v[2]];
  return tris.map((t) =>
    faced({ v: [flip(t.v[0]), flip(t.v[1]), flip(t.v[2])], cell: t.cell, out: flip(t.out) }),
  );
}

/** A swatch's rectangle in an atlas of size px a side, as pixel bounds. */
function bounds(swatch: string, px: number): [number, number, number, number] {
  const [col, row] = swatch.split(',').map(Number) as [number, number];
  return [
    (col * px) / COLUMNS,
    ((col + 1) * px) / COLUMNS,
    (row * px) / ROWS,
    ((row + 1) * px) / ROWS,
  ];
}

/** An atlas as raw RGB pixels: flat swatches on the 8 × 4 grid, grey where none is given. */
export function atlasRaw(cells: Readonly<Record<string, readonly [string, number]>>): Uint8Array {
  const data = new Uint8Array(ATLAS_PX * ATLAS_PX * 3).fill(0x80);
  for (const [swatch, rgb] of Object.values(cells)) {
    const [x0, x1, y0, y1] = bounds(swatch, ATLAS_PX);
    for (let y = y0; y < y1; y++) {
      for (let x = x0; x < x1; x++) {
        const i = (y * ATLAS_PX + x) * 3;
        data[i] = rgb >> 16;
        data[i + 1] = (rgb >> 8) & 0xff;
        data[i + 2] = rgb & 0xff;
      }
    }
  }
  return data;
}

/** One mesh of one primitive in material: each triangle flat-shaded, its UVs its swatch's centre, so a colourway recolours it whole. */
export function meshOf(
  doc: Document,
  buffer: Buffer,
  material: Material,
  name: string,
  tris: readonly Tri[],
): Mesh {
  const position: number[] = [];
  const normal: number[] = [];
  const uv: number[] = [];
  for (const t of tris) {
    const n = cross(sub(t.v[1], t.v[0]), sub(t.v[2], t.v[0]));
    const len = Math.hypot(...n) || 1;
    const [col, row] = t.cell.split(',').map(Number) as [number, number];
    for (const v of t.v) {
      position.push(...v);
      normal.push(n[0] / len, n[1] / len, n[2] / len);
      uv.push((col + 0.5) / COLUMNS, (row + 0.5) / ROWS);
    }
  }
  const accessor = (type: 'VEC3' | 'VEC2', array: number[]) =>
    doc.createAccessor().setType(type).setBuffer(buffer).setArray(new Float32Array(array));
  const primitive = doc
    .createPrimitive()
    .setAttribute('POSITION', accessor('VEC3', position))
    .setAttribute('NORMAL', accessor('VEC3', normal))
    .setAttribute('TEXCOORD_0', accessor('VEC2', uv))
    .setMaterial(material);
  return doc.createMesh(name).addPrimitive(primitive);
}
