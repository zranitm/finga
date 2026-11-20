import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import type { AssetContainer } from '@babylonjs/core/assetContainer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { Logger } from '@babylonjs/core/Misc/logger';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Models } from './models';

const MODELS = path.resolve(process.cwd(), 'public/models');

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

describe('Models', () => {
  it('lists every model in LICENSES.md, for its licence and credit', () => {
    const files = readdirSync(MODELS, { recursive: true, encoding: 'utf8' })
      .filter((f) => f.endsWith('.glb'))
      .sort();
    const licences = readFileSync(path.join(MODELS, 'LICENSES.md'), 'utf8');
    const listed = [...licences.matchAll(/^\| `(\S+\.glb)` /gm)].map((m) => m[1]).sort();
    expect(files.length).toBeGreaterThan(0);
    expect(listed).toEqual(files);
  });

  it('loads each file once however often it is asked for', async () => {
    const asked: string[] = [];
    const models = new Models(scene, (file) => {
      asked.push(file);
      return new Uint8Array(readFileSync(path.join(MODELS, file)));
    });
    const [a, b] = await Promise.all([
      models.load('quaternius/Mushnub.glb'),
      models.load('quaternius/Mushnub.glb'),
    ]);
    expect(a).toBe(b);
    expect(asked).toEqual(['quaternius/Mushnub.glb']);
  });

  it('loads every model without a warning, none with a clip started', async () => {
    const warned = vi.spyOn(Logger, 'Warn');
    const models = new Models(
      scene,
      (file) => new Uint8Array(readFileSync(path.join(MODELS, file))),
    );
    const files = readdirSync(MODELS, { recursive: true, encoding: 'utf8' }).filter((f) =>
      f.endsWith('.glb'),
    );
    const loaded = await Promise.all(files.map((f) => models.load(f)));
    expect(warned.mock.calls.map((c) => String(c[0]))).toEqual([]);
    expect(
      loaded.flatMap((c) => c.animationGroups.filter((g) => g.isPlaying).map((g) => g.name)),
    ).toEqual([]);
  });

  it('holds a file at rest, so a body cloned from it stands as tall whenever it’s made', async () => {
    scene.useConstantAnimationDeltaTime = true; // each animate() a 16 ms step
    const models = new Models(
      scene,
      (file) => new Uint8Array(readFileSync(path.join(MODELS, file))),
    );
    // the files whose first clips moved them most: a chop, a block, and a death that lays a mob down
    const files = ['kaykit/Barbarian.glb', 'kaykit/Mage.glb', 'quaternius/Big_Bunny.glb'];
    const loaded = await Promise.all(files.map((f) => models.load(f)));
    const first = loaded.map(heightOfClone);
    for (let i = 1; i <= 6; i++) {
      for (let k = 0; k < 11; k++) {
        scene.animate();
      }
      loaded.forEach((c, k) =>
        expect(heightOfClone(c), `${files[k]} at ${i * 176} ms`).toBeCloseTo(first[k]!, 6),
      );
    }
  });

  it('forgets a load that failed, so the next asks again', async () => {
    let bytes: Uint8Array = new Uint8Array([1, 2, 3]); // not a glTF
    const asked: string[] = [];
    const models = new Models(scene, (file) => {
      asked.push(file);
      return bytes;
    });
    await expect(models.load('quaternius/Mushnub.glb')).rejects.toThrow();
    bytes = new Uint8Array(readFileSync(path.join(MODELS, 'quaternius/Mushnub.glb')));
    await expect(models.load('quaternius/Mushnub.glb')).resolves.toBeDefined();
    expect(asked).toEqual(['quaternius/Mushnub.glb', 'quaternius/Mushnub.glb']);
  });
});

/** The height of a clone of c's model as its source poses it, skins included. */
function heightOfClone(c: AssetContainer): number {
  const made = c.instantiateModelsToScene((n) => n, false, { doNotInstantiate: true });
  const root = made.rootNodes[0]!;
  for (const m of root.getChildMeshes<AbstractMesh>(false)) {
    if (m.skeleton) {
      m.skeleton.prepare(true);
      m.refreshBoundingInfo({ applySkeleton: true });
    }
  }
  const { min, max } = root.getHierarchyBoundingVectors(true);
  made.dispose();
  return max.y - min.y;
}
