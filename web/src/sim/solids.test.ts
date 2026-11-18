import fc from 'fast-check';
import { describe, expect, it } from 'vitest';

import type { Input } from './input';
import { indexed, near } from './solids';
import { blink, ground, step } from './step';
import type { Geometry, Params, State } from './step';
import { terrain } from './terrain';

/** The sim's bucket side, whose lines the solids are drawn across. */
const BUCKET_M = 8;

const params: Params = {
  dt: 1 / 60,
  walk_speed: 5,
  jump_velocity: 12.34,
  gravity: 38,
  max_fall_speed: 22.5,
  radius: 0.375,
  height: 1.75,
  step: 0.3,
  max_slope: 0.7,
  snap: 0.5,
};

const keys = (k: Partial<Input>): Input => ({
  left: false,
  right: false,
  up: false,
  down: false,
  jump: false,
  ...k,
});

/** Flat ground size metres square. */
function flat(size: number): Geometry {
  return {
    size_x: size,
    size_z: size,
    terrain: terrain(size, 2, 2, [0, 0, 0, 0], params.max_slope),
    boxes: [],
    cylinders: [],
  };
}

const standing = (x: number, z: number): State => ({
  x,
  y: 0,
  z,
  vx: 0,
  vy: 0,
  vz: 0,
  on_ground: true,
  hx: 0,
  hz: 1,
});

/** A map 2 to 120 m a side with rolling ground and solids of every size and turn, some square to the buckets with an edge just across a bucket line, some past the map's edges: as simtest draws the Go side's. */
function drawGeometry(gen: fc.GeneratorValue): Geometry {
  const cell = gen(() => fc.constantFrom(0.5, 1, 2, 4));
  const nx = gen(fc.integer, { min: 2, max: 120 / cell });
  const nz = gen(fc.integer, { min: 2, max: 120 / cell });
  const amp = gen(fc.double, { min: 0, max: 1.5, noNaN: true });
  const fx = gen(fc.double, { min: 0, max: 0.6, noNaN: true });
  const fz = gen(fc.double, { min: 0, max: 0.6, noNaN: true });
  const heights: number[] = [];
  for (let iz = 0; iz <= nz; iz++) {
    for (let ix = 0; ix <= nx; ix++) {
      heights.push(amp * Math.sin(ix * cell * fx) * Math.cos(iz * cell * fz));
    }
  }
  const sizeX = nx * cell;
  const sizeZ = nz * cell;
  const size = () =>
    gen(fc.double, { min: 0.05, max: 1.2, noNaN: true }) *
    gen(() => fc.constantFrom(1, 1, 1, 4, 20));
  const at = (side: number, reach: number) =>
    gen(fc.boolean)
      ? BUCKET_M * gen(fc.integer, { min: 0, max: Math.floor(side / BUCKET_M) + 1 }) +
        gen(() => fc.constantFrom(-1, 1)) *
          (reach + gen(fc.double, { min: 0, max: 0.15, noNaN: true }))
      : gen(fc.double, { min: -4, max: side + 4, noNaN: true });
  const top = () => gen(fc.double, { min: -1.5, max: 4, noNaN: true });
  const boxes = Array.from({ length: gen(fc.integer, { min: 0, max: 40 }) }, () => {
    const w = size();
    const d = size();
    const turn = gen(fc.boolean) ? gen(fc.double, { min: 0, max: 2 * Math.PI, noNaN: true }) : 0;
    return {
      x: at(sizeX, w),
      z: at(sizeZ, d),
      half_w: w,
      half_d: d,
      ux: Math.cos(turn),
      uz: Math.sin(turn),
      top: top(),
    };
  });
  const cylinders = Array.from({ length: gen(fc.integer, { min: 0, max: 40 }) }, () => {
    const r = size();
    return { x: at(sizeX, r), z: at(sizeZ, r), radius: r, top: top() };
  });
  const blockers = Array.from({ length: gen(fc.integer, { min: 0, max: 4 }) }, () => {
    const r = size();
    const bottom = top();
    return { x: at(sizeX, r), z: at(sizeZ, r), radius: r, bottom, top: bottom + 2 };
  });
  return {
    size_x: sizeX,
    size_z: sizeZ,
    terrain: terrain(cell, nx + 1, nz + 1, heights, params.max_slope),
    boxes,
    cylinders,
    blockers,
  };
}

/** A point of g, half the time beside one of its solids, where moves catch on them. */
function drawNear(gen: fc.GeneratorValue, g: Geometry): [number, number] {
  const n = g.boxes.length + g.cylinders.length;
  if (n === 0 || gen(fc.boolean)) {
    return [
      gen(fc.double, { min: 0, max: g.size_x, noNaN: true }),
      gen(fc.double, { min: 0, max: g.size_z, noNaN: true }),
    ];
  }
  const k = gen(fc.integer, { min: 0, max: n - 1 });
  const b = g.boxes[k];
  const c = g.cylinders[k - g.boxes.length];
  const [cx, cz, reach] = b ? [b.x, b.z, Math.hypot(b.half_w, b.half_d)] : [c!.x, c!.z, c!.radius];
  const turn = gen(fc.double, { min: 0, max: 2 * Math.PI, noNaN: true });
  const d = gen(fc.double, { min: 0, max: reach + 1, noNaN: true });
  return [
    Math.min(Math.max(cx + d * Math.cos(turn), 0), g.size_x),
    Math.min(Math.max(cz + d * Math.sin(turn), 0), g.size_z),
  ];
}

// large-regions.md section 5, as the Go side's tests: the TS buckets answer as a full scan, to the bit.
describe('indexed', () => {
  it("gives a NaN body or point, as a broken state might hold, a full scan's answer", () => {
    fc.assert(
      fc.property(fc.gen(), (gen) => {
        const g = drawGeometry(gen);
        const ix = indexed(g);
        let [x, z] = drawNear(gen, g);
        const which = gen(fc.integer, { min: 0, max: 2 });
        x = which === 1 ? x : NaN;
        z = which === 0 ? z : NaN;
        expect(ground(x, z, 2, ix)).toBe(ground(x, z, 2, g));
        const s = standing(x, z);
        const held = keys({ left: gen(fc.boolean), up: gen(fc.boolean), jump: gen(fc.boolean) });
        expect(step(s, held, ix, params)).toEqual(step(s, held, g, params));
        const w = { x: 0.6, z: 0.8, length: 5 };
        expect(blink(s, w, ix, params)).toEqual(blink(s, w, g, params));
      }),
      { numRuns: 30, endOnFailure: true }, // a gen() run draws about a thousand values, and shrinking them runs out of memory
    );
  });

  it('answers every ground query and moves every body as a full scan does', () => {
    fc.assert(
      fc.property(fc.gen(), (gen) => {
        const g = drawGeometry(gen);
        const ix = indexed(g);
        for (let k = 0; k < 20; k++) {
          const [x, z] = drawNear(gen, g);
          const below = gen(fc.double, { min: -2, max: 5, noNaN: true });
          expect(ground(x, z, below, ix)).toBe(ground(x, z, below, g));
        }
        const [x, z] = drawNear(gen, g);
        let s: State = { ...standing(x, z), y: ground(x, z, Infinity, g) };
        let held = keys({});
        for (let k = 0; k < 100; k++) {
          const what = gen(fc.integer, { min: 0, max: 15 });
          let a: State;
          let b: State;
          if (what === 0) {
            const turn = gen(fc.double, { min: 0, max: 2 * Math.PI, noNaN: true });
            const w = {
              x: Math.cos(turn),
              z: Math.sin(turn),
              length: gen(fc.double, { min: 0, max: 12, noNaN: true }),
            };
            a = blink(s, w, g, params);
            b = blink(s, w, ix, params);
          } else {
            if (what === 1) {
              held = keys({
                left: gen(fc.boolean),
                right: gen(fc.boolean),
                up: gen(fc.boolean),
                down: gen(fc.boolean),
                jump: gen(fc.boolean),
              });
            }
            a = step(s, held, g, params);
            b = step(s, held, ix, params);
          }
          expect(b).toEqual(a);
          s = a;
        }
      }),
      { numRuns: 30, endOnFailure: true }, // a gen() run draws about a thousand values, and shrinking them runs out of memory
    );
  });

  it('reads the buckets a move sweeps into: each way toward a cylinder 2 cm across the line at 16 m, a body stops at it', () => {
    const line = 16;
    const r = 0.4;
    const reach = r + 0.02;
    const from = params.radius + 0.025;
    for (const [way, cx, cz, x, z] of [
      [keys({ right: true }), line + reach, 12, line - from, 12],
      [keys({ left: true }), line - reach, 12, line + from, 12],
      [keys({ up: true }), 12, line + reach, 12, line - from],
      [keys({ down: true }), 12, line - reach, 12, line + from],
    ] as const) {
      const g = { ...flat(32), cylinders: [{ x: cx, z: cz, radius: r, top: 2 }] };
      const ix = indexed(g);
      let a = standing(x, z);
      let b = standing(x, z);
      for (let k = 0; k < 6; k++) {
        a = step(a, way, g, params);
        b = step(b, way, ix, params);
      }
      expect(b).toEqual(a);
      expect(Math.hypot(a.x - cx, a.z - cz)).toBeGreaterThanOrEqual(r + params.radius - 1e-9);
      expect(Math.hypot(a.x - x, a.z - z)).toBeLessThanOrEqual(0.045 + 1e-9);
    }
  });

  it('finds a box holding a point a rounding step past its bounds, which start on the bucket line at 16 m', () => {
    const g = {
      ...flat(32),
      boxes: [
        {
          x: 19.43359508418376,
          z: 12,
          half_w: 3.375,
          half_d: 4,
          ux: -0.9998913537933956,
          uz: 0.01474044128276302,
          top: 1,
        },
      ],
    };
    expect(ground(15.999999999999998, 8.050183574155742, 2, indexed(g))).toBe(1);
    expect(ground(15.999999999999998, 8.050183574155742, 2, g)).toBe(1);
  });

  it('gives a query over several buckets each solid once, in index order', () => {
    const box = (x: number, z: number) => ({
      x,
      z,
      half_w: 0.5,
      half_d: 0.5,
      ux: 1,
      uz: 0,
      top: 1,
    });
    const cylinder = (x: number, z: number, radius: number) => ({ x, z, radius, top: 1 });
    const g = indexed({
      ...flat(32),
      boxes: [box(20, 20), box(16, 16), box(9, 9), box(4, 20)],
      cylinders: [cylinder(12, 12, 6), cylinder(20, 4, 1), cylinder(10, 10, 0.5)],
    });
    expect([...near(g, false, 4, 4, 23, 23)]).toEqual([0, 1, 2, 3]);
    expect([...near(g, false, 15, 15, 17, 17)]).toEqual([0, 1, 2]); // the buckets from 8 to 24 m, so not the box at x 4
    expect([...near(g, true, 4, 4, 23, 23)]).toEqual([0, 1, 2]);
    expect([...near(g, true, 9, 9, 17, 17)]).toEqual([0, 2]);
  });

  it('turns a body at a solid reaching into its bucket from another, or from past the far edge', () => {
    const cases: [string, Geometry, State][] = [
      [
        'a cylinder across the line at 16 m, beside a body walking along it',
        { ...flat(32), cylinders: [{ x: 17, z: 10, radius: 2, top: 2 }] },
        standing(15.2, 4),
      ],
      [
        'a cylinder past the east edge',
        { ...flat(30), cylinders: [{ x: 31, z: 10, radius: 2, top: 2 }] },
        standing(29.2, 4),
      ],
      [
        'a box past the north edge',
        {
          ...flat(30),
          boxes: [{ x: 10, z: 31.5, half_w: 1, half_d: 2, ux: 1, uz: 0, top: 2 }],
        },
        standing(10, 22),
      ],
    ];
    const walk = (g: Geometry, start: State) => {
      let s = start;
      for (let k = 0; k < 120; k++) {
        s = step(s, keys({ up: true }), g, params);
      }
      return s;
    };
    for (const [name, g, start] of cases) {
      const scanned = walk(g, start);
      expect(walk(indexed(g), start), name).toEqual(scanned);
      expect(scanned, name).not.toEqual(walk({ ...g, boxes: [], cylinders: [] }, start)); // the solid met it
    }
  });

  it('lists a solid in every bucket it reaches: a box at each turn and a cylinder past the lines at 16 m, every 5 cm', () => {
    const shapes: Geometry[] = [0, 0.5, Math.PI / 4, 1.2, Math.PI / 2, 2.3, (3 * Math.PI) / 4].map(
      (turn) => ({
        ...flat(32),
        boxes: [
          {
            x: 14,
            z: 14,
            half_w: 2.5,
            half_d: 0.8,
            ux: Math.cos(turn),
            uz: Math.sin(turn),
            top: 1,
          },
        ],
      }),
    );
    shapes.push({ ...flat(32), cylinders: [{ x: 14, z: 14, radius: 2.5, top: 1 }] });
    for (const g of shapes) {
      const ix = indexed(g);
      for (let x = 10; x <= 18; x += 0.05) {
        for (let z = 10; z <= 18; z += 0.05) {
          if (ground(x, z, 2, ix) !== ground(x, z, 2, g)) {
            throw new Error(
              `${JSON.stringify(g.boxes)} ${JSON.stringify(g.cylinders)}: ground at (${x}, ${z}) differs`,
            );
          }
        }
      }
    }
  });
});
