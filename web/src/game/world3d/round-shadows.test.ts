import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { indexed } from '@/sim/solids';
import type { Geometry } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { RoundShadows } from './round-shadows';
import { SHADOW_ORDER } from './see-through';
import { Palette } from './toon';
import { Torchlight } from './torchlight';

let engine: NullEngine;
let scene: Scene;
let round: RoundShadows;
const disc = () => scene.getMeshByName('round shadows') as Mesh;

let matrices: Float32Array; // the instances' buffer, as the disc was last handed it
const SHADOW_M = 25; // round-shadows' reach

/** Flat ground at 0 that turns into a hillside past a diagonal, rising 0.5 a metre along x and 0.25 along z, 60 by 40 m, a metre a sample; on the hillside a box at (40, 30) whose top, 16, stands 3.5 m over it. */
const h = (x: number, z: number) => Math.max(0, 0.5 * (x - 20) + 0.25 * (z - 20));
const ground: Geometry = indexed({
  size_x: 60,
  size_z: 40,
  terrain: terrain(
    1,
    61,
    41,
    Array.from({ length: 61 * 41 }, (_, i) => h(i % 61, Math.floor(i / 61))),
    movement.max_slope,
  ),
  boxes: [{ x: 40, z: 30, half_w: 2, half_d: 2, ux: 1, uz: 0, top: 16 }],
  cylinders: [],
});

/** Each drawn disc's x, y and z, and its radius. */
function drawn(): [number, number, number, number][] {
  const m = matrices;
  return Array.from({ length: disc().thinInstanceCount }, (_, i) => {
    const o = 16 * i;
    return [m[o + 12]!, m[o + 13]!, m[o + 14]!, m[o]!];
  });
}

beforeEach(() => {
  engine = new NullEngine();
  engine.getCaps().instancedArrays = true;
  scene = new Scene(engine);
  const set = Object.getOwnPropertyDescriptor(Mesh.prototype, 'thinInstanceSetBuffer')!
    .value as Mesh['thinInstanceSetBuffer']; // the unspied one, to call through
  vi.spyOn(Mesh.prototype, 'thinInstanceSetBuffer').mockImplementation(function (
    this: Mesh,
    ...args
  ) {
    matrices = args[1]!;
    set.apply(this, args);
  });
  round = new RoundShadows(scene, new Palette(scene, new Torchlight(scene, null)), {
    geometry: ground,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

describe('a cave’s round shadows (underground-and-underwater.md section 7)', () => {
  it('lays a soft dark disc flat on the ground under each body near the camera’s target, a little wider than it', () => {
    round.begin(10, 10);
    round.add(12, 1.5, 10, 0.5); // in the air: its disc on the ground under it
    round.add(10 + SHADOW_M + 0.1, 0, 10, 0.5); // past the reach
    round.add(10, 0, 10 + SHADOW_M - 0.1, 1);
    round.end();
    expect(disc().thinInstanceCount).toBe(2);
    expect(drawn()[0]).toEqual([12, expect.closeTo(0.04, 6), 10, expect.closeTo(0.8, 6)]);
    expect(drawn()[1]).toEqual([
      10,
      expect.closeTo(0.04, 6),
      expect.closeTo(10 + SHADOW_M - 0.1, 5),
      expect.closeTo(1.6, 6),
    ]);
    const m = disc().material!;
    expect([m.needAlphaBlending(), m.disableDepthWrite, disc().isPickable]).toEqual([
      true,
      true,
      false,
    ]);
    expect([disc().alphaIndex, m.stencil.funcRef]).toEqual([SHADOW_ORDER, 0]); // under a red area, clearing the stencil as the ground does
  });

  it('draws only the frame’s own, however many came before', () => {
    round.begin(0, 0);
    for (let i = 0; i < 100; i++) {
      round.add(i / 10, 0, 0, 0.5);
    }
    round.end();
    expect(disc().thinInstanceCount).toBe(100);
    expect(drawn()[99]![0]).toBeCloseTo(9.9);
    round.begin(0, 0);
    round.add(3, 0, 4, 0.5);
    round.end();
    expect(disc().thinInstanceCount).toBe(1);
    expect(drawn()[0]!.slice(0, 3)).toEqual([3, expect.closeTo(0.04, 6), 4]);
  });

  it('lies on a platform its body stands on, and on the terrain under one jumping beside it', () => {
    round.begin(40, 30);
    round.add(40, 16, 30, 0.5);
    round.add(45, 20, 30, 0.5); // in the air past the platform's edge, over the hillside at 15
    round.end();
    expect(drawn().map((d) => d[1])).toEqual([expect.closeTo(16.04, 5), expect.closeTo(15.04, 5)]);
  });

  it('lies sheared to a hillside’s slope, so its uphill edge isn’t buried, and flat on a platform there', () => {
    round.begin(30, 24);
    round.add(30, h(30, 24), 24, 0.5);
    round.add(40, 16, 30, 0.5);
    round.end();
    const s = 0.5 * 1.6;
    const close = (v: number): unknown => expect.closeTo(v, 6);
    expect(Array.from(matrices.subarray(0, 12))).toEqual(
      [s, s * 0.5, 0, 0, 0, 1, 0, 0, 0, s * 0.25, s, 0].map(close),
    );
    expect([matrices[16 + 1], matrices[16 + 9]]).toEqual([0, 0]);
  });
});
