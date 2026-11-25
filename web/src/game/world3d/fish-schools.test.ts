import { readFileSync } from 'node:fs';
import path from 'node:path';

import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FishSchools } from './fish-schools';
import { CHEST_M } from './follow';
import { Models } from './models';
import { Palette } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');

let engine: NullEngine;
let scene: Scene;
let buffer: Float32Array; // the fish's instances, as handed their meshes: Babylon keeps its world matrices from the first read

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const set = Object.getOwnPropertyDescriptor(Mesh.prototype, 'thinInstanceSetBuffer')!
    .value as Mesh['thinInstanceSetBuffer']; // the unspied one, to call through
  vi.spyOn(Mesh.prototype, 'thinInstanceSetBuffer').mockImplementation(function (
    this: Mesh,
    ...args
  ) {
    buffer = args[1]!;
    set.apply(this, args);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

/** A fish as drawn: where, the way its nose points, and its size against the model's. */
interface Drawn {
  readonly at: Vector3;
  readonly nose: Vector3;
  readonly scale: number;
}

function fish(meshes: readonly Mesh[]): Drawn[] {
  return Array.from({ length: meshes[0]!.thinInstanceCount }, (_, i) => {
    const m = buffer.subarray(16 * i, 16 * i + 16);
    const nose = new Vector3(m[8], m[9], m[10]);
    return {
      at: new Vector3(m[12], m[13], m[14]),
      nose: nose.normalizeToNew(),
      scale: nose.length(),
    };
  });
}

/** Schools of a sea, their model loaded from disk. */
async function schools(): Promise<{ schools: FishSchools; meshes: Mesh[]; reach: number }> {
  const models = new Models(scene, (f) => new Uint8Array(readFileSync(path.join(MODELS, f))));
  const schools = new FishSchools(scene, new Palette(scene, 'sea'), models);
  await vi.waitFor(() => {
    expect(scene.meshes.some((m) => m.name.startsWith('fish '))).toBe(true);
  });
  const meshes = scene.meshes.filter(
    (m): m is Mesh => m instanceof Mesh && m.name.startsWith('fish ') && m.thinInstanceCount > 0,
  );
  const reach = Math.max(
    ...meshes.flatMap((m) => {
      const v = m.getVerticesData('position')!;
      return Array.from({ length: v.length / 3 }, (_, k) =>
        Math.hypot(v[3 * k]!, v[3 * k + 1]!, v[3 * k + 2]!),
      );
    }),
  ); // how far the model reaches from its origin, where an instance stands
  return { schools, meshes, reach };
}

/** The camera 16 m behind a chest on a yaw, as the follow camera stands, at its height for a pitch. */
function eyeOn(chest: Vector3, yaw: number, pitch: number, back = 16): Vector3 {
  return new Vector3(
    chest.x - back * Math.cos(pitch) * Math.sin(yaw),
    chest.y + back * Math.sin(pitch),
    chest.z - back * Math.cos(pitch) * Math.cos(yaw),
  );
}

const slope = (x: number, z: number) => 0.05 * x - 0.03 * z; // a sea floor that isn't flat

describe('a sea’s fish (underground-and-underwater.md section 7)', () => {
  it('swim in small schools of a dozen, one model thin-instanced, drawn in the sea’s light', async () => {
    const { schools: s, meshes } = await schools();
    const chest = new Vector3(20, slope(20, 30) + CHEST_M, 30);
    s.frame(10, eyeOn(chest, 0, 0.7), chest, slope);
    const n = meshes[0]!.thinInstanceCount;
    expect(n % 12).toBe(0);
    expect(n / 12).toBeGreaterThanOrEqual(2);
    expect(n / 12).toBeLessThanOrEqual(5); // a few small schools
    for (const m of meshes) {
      expect(m.thinInstanceCount).toBe(n);
      expect((m.material as NodeMaterial).getBlockByName('causticCells')).not.toBeNull();
    }
  });

  it('circle 4–8 m over the floor, nose first and slowly, round the player as they go', async () => {
    const { schools: s, meshes } = await schools();
    let fastest = 0;
    let last: Drawn[] | null = null;
    for (let t = 0; t < 120; t += 0.25) {
      const chest = new Vector3(t * 2, slope(t * 2, -t) + CHEST_M, -t); // the player walking
      s.frame(t, eyeOn(chest, 0, 1.2, 40), chest, slope); // the camera far up, out of their way
      const now = fish(meshes);
      now.forEach((f, i) => {
        if (f.scale === 0) {
          return;
        }
        const up = f.at.y - slope(f.at.x, f.at.z);
        expect(up).toBeGreaterThan(3); // over the floor where it swims, on a slope
        expect(Math.hypot(f.at.x - chest.x, f.at.z - chest.z)).toBeLessThan(40);
        const was = last?.[i];
        if (was && was.scale > 0 && Vector3.Distance(f.at, was.at) < 5) {
          const moved = f.at.subtract(was.at);
          fastest = Math.max(fastest, Math.hypot(moved.x, moved.z) / 0.25);
          expect(Vector3.Dot(moved.normalize(), f.nose)).toBeGreaterThan(0.9); // nose first
        }
      });
      last = now;
    }
    expect(fastest).toBeGreaterThan(0.5);
    expect(fastest).toBeLessThan(2.5); // slowly
    const still = new Vector3(0, CHEST_M, 0);
    for (let t = 0; t < 60; t += 0.5) {
      s.frame(t, eyeOn(still, 0, 1.2, 40), still, () => 0);
      for (const f of fish(meshes).filter((f) => f.scale > 0)) {
        expect(f.at.y).toBeGreaterThanOrEqual(4);
        expect(f.at.y).toBeLessThanOrEqual(8);
      }
    }
  });

  it('never cross the camera’s line to the player’s chest, wherever they stand and look', async () => {
    const { schools: s, meshes, reach } = await schools();
    let near = 0;
    for (let k = 0; k < 600; k++) {
      const chest = new Vector3(((k * 37) % 300) - 150, CHEST_M, ((k * 53) % 300) - 150);
      const eye = eyeOn(chest, k * 0.61, 0.2 + (k % 5) * 0.25, 6 + (k % 4) * 10);
      s.frame(k * 1.37, eye, chest, () => 0);
      for (const f of fish(meshes).filter((f) => f.scale > 0)) {
        const gap = Math.min(
          ...Array.from({ length: 201 }, (_, i) =>
            Vector3.Distance(f.at, Vector3.Lerp(eye, chest, i / 200)),
          ),
        );
        expect(gap - reach * f.scale).toBeGreaterThan(0.05); // the line's sampled every few centimetres
        if (gap < 3) {
          near++;
        }
      }
    }
    expect(near).toBeGreaterThan(0); // some came close, so the check bore on them
  });
});
