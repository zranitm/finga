import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import type { Input } from './input';
import { NO_PLATFORM, faster, overlaps, params, speedFactor, standing, step } from './step';
import type { Geometry, State } from './step';

const noInput: Input = { left: false, right: false, up: false, down: false, jump: false };

// The vectors are the contract with the Go sim: every intermediate state must match exactly.
interface Vector {
  readonly name: string;
  readonly speed_factor?: number;
  readonly geometry: Geometry;
  readonly start: State;
  readonly inputs: readonly { readonly input: Partial<Input>; readonly repeat: number }[];
  readonly expected: readonly State[];
}

const vectorsDir = path.resolve(process.cwd(), '..', 'server', 'testdata', 'movement');
const vectors = readdirSync(vectorsDir)
  .filter((name) => name.endsWith('.json'))
  .map((name) => ({
    file: name,
    vector: JSON.parse(readFileSync(path.join(vectorsDir, name), 'utf8')) as Vector,
  }));

const required = [
  'ceiling',
  'climb-rope',
  'climb-rope-fast',
  'drop-through',
  'fall-clamp',
  'fall-clamp-fast',
  'jump-onto-platform-fast',
  'jump-through-platform',
  'jump',
  'land-on-platform',
  'ledge',
  'platform-edge',
  'walk-fast',
  'walk-flat',
  'wall',
];

// Go treats -0 and 0 as equal but JSON keeps the sign, so normalise before comparing.
function normalise(s: State): State {
  const n = (x: number): number => (x === 0 ? 0 : x);
  return { ...s, x: n(s.x), y: n(s.y), vx: n(s.vx), vy: n(s.vy), drop_timer: n(s.drop_timer) };
}

describe('movement vectors', () => {
  it('include every required scenario', () => {
    const names = vectors.map((v) => v.file.replace(/\.json$/, ''));
    for (const name of required) {
      expect(names).toContain(name);
    }
  });

  it.each(vectors)('$file replays exactly', ({ file, vector }) => {
    expect(vector.name).toBe(file.replace(/\.json$/, ''));
    let s = vector.start;
    const p = faster(params, vector.speed_factor ?? 1);
    const got: State[] = [];
    for (const run of vector.inputs) {
      for (let k = 0; k < run.repeat; k++) {
        s = step(s, { ...noInput, ...run.input }, vector.geometry, p);
        got.push(s);
      }
    }
    expect(got).toHaveLength(vector.expected.length);
    got.forEach((state, i) => {
      expect(normalise(state), `sub-step ${i + 1}`).toEqual(normalise(vector.expected[i]!));
    });
  });
});

describe('speed', () => {
  it('rounds the factor to thousandths, as the server does', () => {
    expect([0, 0.3, 0.007317073170731708, 0.12345].map(speedFactor)).toEqual([
      1, 1.3, 1.007, 1.123,
    ]);
  });

  it('leaves the params alone at a factor of 1', () => {
    expect(faster(params, 1)).toBe(params);
  });
});

describe('step', () => {
  const floor: Geometry = { solids: [{ x: 0, y: 500, w: 1000, h: 32 }], platforms: [], ropes: [] };

  it('reads its constants from the generated gamedata', () => {
    expect(params).toMatchObject({
      walk_speed: 160,
      jump_velocity: 496,
      gravity: 1216,
      width: 24,
      height: 56,
    });
    expect(params.dt).toBeCloseTo(1 / 60, 12);
  });

  it('never changes its argument', () => {
    const before = standing(200, 500);
    const frozen = { ...before };
    step(before, { ...noInput, right: true, jump: true }, floor);
    expect(before).toEqual(frozen);
  });

  it.each([
    {
      name: 'right wins over left for facing',
      input: { left: true, right: true },
      facing: 1,
      vx: 0,
    },
    { name: 'left alone faces and walks left', input: { left: true }, facing: -1, vx: -160 },
    { name: 'nothing held keeps facing', input: {}, facing: 1, vx: 0 },
  ])('$name', ({ input, facing, vx }) => {
    const s = step(standing(200, 500), { ...noInput, ...input }, floor);
    expect(s.facing).toBe(facing);
    expect(s.vx).toBe(vx);
    expect(s.ground).toBe(NO_PLATFORM);
  });

  it.each([
    {
      name: 'touching edges do not overlap',
      a: { x: 0, y: 0, w: 10, h: 10 },
      b: { x: 10, y: 0, w: 10, h: 10 },
      want: false,
    },
    {
      name: 'a shared corner does not overlap',
      a: { x: 0, y: 0, w: 10, h: 10 },
      b: { x: 10, y: 10, w: 10, h: 10 },
      want: false,
    },
    {
      name: 'a one pixel overlap does',
      a: { x: 0, y: 0, w: 10, h: 10 },
      b: { x: 9, y: 9, w: 10, h: 10 },
      want: true,
    },
  ])('$name', ({ a, b, want }) => {
    expect(overlaps(a, b)).toBe(want);
  });
});
