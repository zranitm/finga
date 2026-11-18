import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import type { Input } from './input';
import { blink, dash, endSprint, faster, fighting, step } from './step';
import type { Blocker, Geometry, Mobs, Params, Slow, State } from './step';
import { terrain } from './terrain';
import type { Terrain } from './terrain';

// The vectors are the contract with the Go sim: every state after every sub-step must match to the bit; toEqual tells -0 from +0.
interface Vector {
  readonly name: string;
  readonly params: Params;
  readonly speed_factor?: number;
  readonly geometry: Omit<Geometry, 'terrain'> & {
    readonly terrain: Pick<Terrain, 'cell' | 'nx' | 'nz' | 'heights'>;
  };
  readonly start: State;
  /** blockers, when set, are the mobs from that run on; held and slow, when set, a status the world sets before its first sub-step; breaks, a break-free ending the hold after them; ends_sprint, an attack or skill pressed; move, when set, starts last; fighting steps them as a body in combat. */
  readonly inputs: readonly {
    readonly input: Partial<Input>;
    readonly repeat: number;
    readonly blockers?: readonly Blocker[];
    readonly held?: number;
    readonly slow?: Slow;
    readonly breaks?: boolean;
    readonly ends_sprint?: boolean;
    readonly fighting?: boolean;
    readonly move?: {
      readonly kind: 'dash' | 'leap' | 'blink';
      readonly ux: number;
      readonly uz: number;
      readonly length: number;
      readonly steps?: number;
      readonly stop?: boolean;
      readonly through?: boolean;
    };
  }[];
  readonly expected: readonly State[];
}

/** How a vector's move meets mobs' bodies. */
function mobsOf(m: { readonly stop?: boolean; readonly through?: boolean }): Mobs {
  return m.through ? 'through' : m.stop ? 'stop' : 'slide';
}

const vectorsDir = path.resolve(process.cwd(), '..', 'server', 'testdata', 'movement');
const vectors = readdirSync(vectorsDir)
  .filter((file) => file.endsWith('.json'))
  .map((file) => ({
    file,
    ...(JSON.parse(readFileSync(path.join(vectorsDir, file), 'utf8')) as Vector),
  }));

// docs/tech/3d-world.md §5, as server/internal/shared/sim's requiredVectors lists them.
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
  'round-cylinder',
  'round-cylinder-east',
  'round-cylinder-top',
  'round-cylinder-top-north',
  'round-cylinder-top-west',
  'round-cylinder-top-south',
  'cylinder-middle',
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
  'dash-open',
  'dash-into-box',
  'dash-along-box',
  'dash-up-cliff',
  'dash-off-mesa',
  'dash-at-mob',
  'dash-along-mob',
  'dash-ends-mid-input',
  'leap-open',
  'leap-into-box',
  'leap-along-box',
  'leap-up-cliff',
  'leap-off-mesa',
  'leap-at-mob',
  'blink-open',
  'blink-into-box',
  'blink-along-box',
  'blink-up-cliff',
  'blink-off-mesa',
  'blink-at-edge',
  'blink-through-mob',
  'blink-behind-mob',
  'blink-into-mob',
  'held-keys',
  'held-mid-air',
  'held-mid-dash',
  'slow-walk',
  'slow-walk-fast',
  'slow-dash',
  'held-and-slow-run-out',
  'leap-breaks-free',
  'blink-breaks-free',
  'leap-through-mob',
  'leap-through-into-mob',
  'leap-through-touching-mobs',
  'leap-through-to-gap',
  'leap-through-into-box',
  'leap-through-never-clear',
  'leap-through-round-trunk',
  'sprint-after-walking',
  'sprint-jump-keeps',
  'sprint-fast',
  'sprint-and-combat',
  'sprint-ends-on-press',
  'sprint-two-presses',
];

const noKeys: Input = { left: false, right: false, up: false, down: false, jump: false };

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
    if (run.held) {
      s = { ...s, held: run.held };
    }
    if (run.slow) {
      s = { ...s, slow: run.slow };
    }
    if (run.breaks) {
      s = { ...s, held: 0 };
    }
    if (run.ends_sprint) {
      s = endSprint(s);
    }
    const m = run.move;
    if (m) {
      const way = { x: m.ux, z: m.uz, length: m.length };
      s =
        m.kind === 'blink'
          ? blink(s, way, g, p)
          : dash(s, way, m.steps ?? 0, mobsOf(m), m.kind === 'leap', p);
    }
    const stepping = run.fighting ? fighting(p) : p;
    for (let i = 0; i < run.repeat; i++) {
      s = step(s, { ...noKeys, ...run.input }, g, stepping);
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
      expect(v.file).toBe(`${v.name}.json`);
      const got = replay(v);
      expect(got).toHaveLength(v.expected.length);
      got.forEach((s, i) => {
        expect(s, `sub-step ${i + 1}`).toEqual(v.expected[i]!);
      });
    });
  }
});
