import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { RoundShadows, SHADOW_M } from './round-shadows';
import { Palette } from './toon';
import { Torchlight } from './torchlight';

let engine: NullEngine;
let scene: Scene;
let round: RoundShadows;
const disc = () => scene.getMeshByName('round shadows') as Mesh;

let matrices: Float32Array; // the instances' buffer, as the disc was last handed it

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
  round = new RoundShadows(scene, new Palette(scene, new Torchlight(scene, null)));
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

describe('a cave’s round shadows (underground-and-underwater.md section 7)', () => {
  it('lays a soft dark disc flat on the ground under each body near the camera’s target, a little wider than it', () => {
    round.begin(10, 10);
    round.add(12, 1.5, 10, 0.5);
    round.add(10 + SHADOW_M + 0.1, 0, 10, 0.5); // past the reach
    round.add(10, 0, 10 + SHADOW_M - 0.1, 1);
    round.end();
    expect(disc().thinInstanceCount).toBe(2);
    expect(drawn()[0]).toEqual([12, expect.closeTo(1.54, 6), 10, expect.closeTo(0.8, 6)]);
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
});
