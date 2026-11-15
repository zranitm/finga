import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import type { Input } from './input';
import { faster, step } from './step';
import type { Blocker, Geometry, Params, State } from './step';
import { terrain } from './terrain';
import type { Terrain } from './terrain';

// The vectors are the contract with the Go sim: every state after every sub-step must match to the bit.
interface Vector {
  readonly name: string;
  readonly params: Params;
  readonly speed_factor?: number;
  readonly geometry: Omit<Geometry, 'terrain'> & {
    readonly terrain: Pick<Terrain, 'cell' | 'nx' | 'nz' | 'heights'>;
  };
  readonly start: State;
  /** blockers, when set, are the mobs from that run on. */
  readonly inputs: readonly {
    readonly input: Partial<Input>;
    readonly repeat: number;
    readonly blockers?: readonly Blocker[];
  }[];
  readonly expected: readonly State[];
}

const vectorsDir = path.resolve(process.cwd(), '..', 'server', 'testdata', 'movement3d');
const vectors = readdirSync(vectorsDir)
  .filter((name) => name.endsWith('.json'))
  .map((name) => JSON.parse(readFileSync(path.join(vectorsDir, name), 'utf8')) as Vector);

// docs/tech/3d-world.md §5, as server/internal/sim3d's requiredVectors lists them.
const required = [
  'walk-north',
  'walk-north-east',
  'walk-east',
  'walk-south-east',
  'walk-south',
  'walk-south-west',
  'walk-west',
  'walk-north-west',
  'walk-opposite-keys',
  'walk-fast',
  'slide-box',
  'box-corner',
  'slide-box-rotated',
  'box-corner-rotated',
  'slide-cylinder',
  'ramp-up-axis',
  'ramp-up-diagonal',
  'cliff-stop-axis',
  'cliff-stop-diagonal',
  'cliff-straddles-cells',
  'jump-onto-cliff',
  'jump-short-of-cliff',
  'step-off-cliff',
  'land-on-steep-cell',
  'walk-down-slope',
  'jump',
  'jump-onto-box',
  'jump-short-of-box',
  'jump-onto-stump',
  'jump-across-step',
  'step-onto-box',
  'fall-off-mesa',
  'fall-clamp',
  'mob-walk-into',
  'mob-walk-out',
  'mob-walk-along',
  'mob-jump-over',
  'map-edge',
];

const noKeys: Input = { left: false, right: false, up: false, down: false, jump: false };

// Go writes -0 as -0 and JSON.parse keeps it, while a sum here may give 0 for it: compare them as one.
function normalise(s: State): State {
  const n = (v: number): number => (v === 0 ? 0 : v);
  return {
    ...s,
    x: n(s.x),
    y: n(s.y),
    z: n(s.z),
    vx: n(s.vx),
    vy: n(s.vy),
    vz: n(s.vz),
    hx: n(s.hx),
    hz: n(s.hz),
  };
}

function replay(v: Vector): State[] {
  const p = v.speed_factor ? faster(v.params, v.speed_factor) : v.params;
  const t = v.geometry.terrain;
  let g: Geometry = { ...v.geometry, terrain: terrain(t.cell, t.nx, t.nz, t.heights, p.max_slope) };
  let s = v.start;
  const out: State[] = [];
  for (const run of v.inputs) {
    if (run.blockers) {
      g = { ...g, blockers: run.blockers };
    }
    for (let i = 0; i < run.repeat; i++) {
      s = step(s, { ...noKeys, ...run.input }, g, p);
      out.push(s);
    }
  }
  return out;
}

describe('3D movement vectors', () => {
  it('are exactly the required scenarios', () => {
    expect(vectors.map((v) => v.name).sort()).toEqual([...required].sort());
  });

  for (const v of vectors) {
    it(`${v.name} replays to the bit`, () => {
      const got = replay(v);
      expect(got).toHaveLength(v.expected.length);
      got.forEach((s, i) => {
        expect(normalise(s), `sub-step ${i + 1}`).toEqual(normalise(v.expected[i]!));
      });
    });
  }
});
