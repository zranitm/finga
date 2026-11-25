import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Particle } from '@babylonjs/core/Particles/particle';
import { ParticleSystem } from '@babylonjs/core/Particles/particleSystem';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { SeaBubbles } from './sea-bubbles';

let engine: NullEngine;
let scene: Scene;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

const system = (name: string) =>
  scene.particleSystems.find((p) => p.name === name) as ParticleSystem;

/** Where ps starts its next bubble, as Babylon asks it while it emits: a null engine never runs a system, its shaders never in. */
function start(ps: ParticleSystem): Vector3 {
  const at = new Vector3(NaN, NaN, NaN);
  ps.startPositionFunction!(Matrix.Identity(), at, {} as Particle, false);
  return at;
}

describe('a sea’s bubbles (underground-and-underwater.md section 7)', () => {
  it('stream from the vents near the camera’s target, each a thin line rising from its mouth, as one system; none from those the fog hides', () => {
    const bubbles = new SeaBubbles(scene);
    const vents = system('vent bubbles');
    const near = [new Vector3(10, 2.4, 12), new Vector3(-6, 0.4, 20)];
    const far = new Vector3(90, 0.4, 12);
    bubbles.vents(8, 8, [near[0]!, far]);
    const one = vents.emitRate;
    expect(one).toBeGreaterThan(0);
    for (let i = 0; i < 50; i++) {
      const at = start(vents);
      expect(Math.hypot(at.x - near[0]!.x, at.z - near[0]!.z)).toBeLessThan(0.1); // a thin line
      expect(at.y).toBe(near[0]!.y); // from its mouth
    }
    bubbles.vents(8, 8, [...near, far]);
    expect(vents.emitRate).toBe(2 * one);
    const from = new Set(
      Array.from({ length: 200 }, () => {
        const at = start(vents);
        return near.findIndex((v) => Vector3.Distance(at, v) < 0.1);
      }),
    );
    expect([...from].sort()).toEqual([0, 1]); // both stream, and nothing starts elsewhere
    bubbles.vents(8, 8, []);
    expect(vents.emitRate).toBe(0);
    for (const ps of [vents, system('breath bubbles')]) {
      expect(ps.direction1.y).toBeGreaterThan(0.9);
      expect(ps.direction2.y).toBeGreaterThan(0.9);
      expect(ps.gravity.y).toBeGreaterThan(0); // rising, the faster as they go
      expect(ps.blendMode).toBe(ParticleSystem.BLENDMODE_STANDARD); // fogged into the water
    }
  });

  it('rise from a body’s head a few at a time every 3–5 s, within 25 m of the camera’s target', () => {
    const bubbles = new SeaBubbles(scene);
    const heads = system('breath bubbles');
    const bodies = [
      { key: {}, x: 3, z: 4, breaths: [] as { at: number; n: number }[] },
      { key: {}, x: -5, z: 2, breaths: [] as { at: number; n: number }[] },
      { key: {}, x: 30, z: 0, breaths: [] as { at: number; n: number }[] }, // 30 m off
    ];
    for (let now = 0; now <= 40_000; now += 50) {
      for (const b of bodies) {
        const before = Math.max(0, heads.manualEmitCount);
        bubbles.breathe(b.key, b.x, 1.75, b.z, 0, 0, now);
        const n = Math.max(0, heads.manualEmitCount) - before;
        if (n > 0) {
          b.breaths.push({ at: now, n });
          for (let i = 0; i < n; i++) {
            const at = start(heads); // given off at the next frame, in order
            expect(Math.hypot(at.x - b.x, at.z - b.z)).toBeLessThan(0.1);
            expect(at.y).toBe(1.75); // from the head
          }
        }
      }
      heads.manualEmitCount = 0; // as a frame's emitting leaves it
    }
    for (const b of bodies.slice(0, 2)) {
      expect(b.breaths[0]!.at).toBeLessThanOrEqual(5000 + 50);
      for (const [k, breath] of b.breaths.entries()) {
        expect(breath.n).toBeGreaterThanOrEqual(2);
        expect(breath.n).toBeLessThanOrEqual(4);
        if (k > 0) {
          const wait = breath.at - b.breaths[k - 1]!.at;
          expect(wait).toBeGreaterThanOrEqual(3000);
          expect(wait).toBeLessThanOrEqual(5000 + 50);
        }
      }
      expect(b.breaths.length).toBeGreaterThanOrEqual(7);
    }
    expect(bodies[2]!.breaths).toEqual([]);
  });
});
