import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { sunShadows } from './body';
import type { Lighting } from './body';
import { DropMark, warmGems } from './drop';
import { Palette } from './toon';

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

describe('DropMark', () => {
  it('builds each tab’s gem shader on a hidden gem as the map starts, the material its drops draw in, ahead of the first drop of its colour', () => {
    const checks = vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh');
    warmGems(scene, lighting.palette);
    for (const [i, item] of ['wooden_sword', 'red_potion', 'slime_gel'].entries()) {
      new DropMark(scene, item, { x: i, y: 0, z: 0 }, lighting); // one of each tab
    }
    const built = checks.mock.calls.flatMap(([mesh], i) =>
      mesh.name === 'kept gem' ? [(checks.mock.contexts[i] as NodeMaterial).name] : [],
    ); // the null engine shares one shader between them, where WebGL builds one each
    const materials = scene.meshes.filter((m) => m.name === 'drop').map((m) => m.material!.name);
    expect([...new Set(built)].toSorted()).toEqual(materials.toSorted());
    expect(new Set(materials).size).toBe(3);
  });
});
