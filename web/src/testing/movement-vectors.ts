// The movement vectors server/internal/shared/sim writes, read and replayed as the client steps: every state after every sub-step must match Go's to the bit, toEqual telling -0 from +0.
import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import type { Input } from '@/sim/input';
import { indexed } from '@/sim/solids';
import { blink, dash, endSprint, faster, fighting, step } from '@/sim/step';
import type { Blocker, Geometry, Mobs, Params, Slow, State } from '@/sim/step';
import { terrain } from '@/sim/terrain';
import type { Terrain } from '@/sim/terrain';

/** A vector, on its own map's geometry, or on the ground of the test plan region names, whose bake the client keeps in src/testing/regions. */
export interface Vector {
  readonly name: string;
  readonly region?: string;
  readonly params: Params;
  readonly speed_factor?: number;
  readonly geometry?: Omit<Geometry, 'terrain'> & {
    readonly terrain: Pick<Terrain, 'cell' | 'nx' | 'nz' | 'heights'> & {
      readonly deep_water?: readonly boolean[];
    };
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

/** Every vector, with the file it came from. */
export const vectors = readdirSync(vectorsDir)
  .filter((file) => file.endsWith('.json'))
  .map((file) => ({
    file,
    ...(JSON.parse(readFileSync(path.join(vectorsDir, file), 'utf8')) as Vector),
  }));

/** v's own map's geometry, bucketed as a map's is, and as Go replays it. */
export function mapGround(v: Vector): Geometry {
  const t = v.geometry!.terrain;
  return indexed({
    ...v.geometry!,
    terrain: {
      ...terrain(t.cell, t.nx, t.nz, t.heights, v.params.max_slope),
      ...(t.deep_water && { deepWater: Uint8Array.from(t.deep_water, Number) }),
    },
  });
}

const noKeys: Input = { left: false, right: false, up: false, down: false, jump: false };

/** Every state v's inputs step its start through on ground, sub-step by sub-step. */
export function replay(v: Vector, ground: Geometry): State[] {
  const p = v.speed_factor ? faster(v.params, v.speed_factor) : v.params;
  let g = ground;
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
