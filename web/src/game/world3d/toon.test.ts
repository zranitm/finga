import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { Palette, warm } from './toon';

let engine: NullEngine;
let scene: Scene;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  new DirectionalLight('sun', new Vector3(0, -1, 0), scene);
  new TargetCamera('eye', new Vector3(0, 1, -4), scene).setTarget(Vector3.Zero());
});

afterEach(() => {
  vi.restoreAllMocks();
  scene.dispose();
  engine.dispose();
});

/** The scale states, stretched or not, that warm builds each mesh's shader for. */
function warmedFor(fades: Parameters<typeof warm>[0]): string[] {
  const build = Object.getOwnPropertyDescriptor(NodeMaterial.prototype, 'isReadyForSubMesh')!
    .value as NodeMaterial['isReadyForSubMesh']; // the unspied build, to call through
  const seen = new Set<string>();
  vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh').mockImplementation(function (
    this: NodeMaterial,
    ...args
  ) {
    seen.add(`${args[0].name} ${args[0].nonUniformScaling}`);
    return build.apply(this, args);
  });
  warm(fades);
  vi.restoreAllMocks();
  return [...seen].toSorted();
}

// An afterimage's copy is cloned from the model a figure was made from, and carries its stretched state until its world matrix is worked out; the shader follows that state.
it('warms a copy for the scale it is drawn at, not the one it was cloned with', () => {
  const model = CreateBox('model', {}, scene);
  model.scaling.set(1, 1.5, 1);
  model.computeWorldMatrix(true);
  const copy = model.clone('copy');
  copy.scaling.setAll(1);
  model.dispose();
  const faded = new Palette(scene).toon({ texture: null, color: Color3.White() }, 0.5);
  copy.material = faded;
  const warmed = warmedFor([[faded, copy]]);
  scene.render();
  expect(copy.nonUniformScaling).toBe(false);
  expect(warmed).toEqual(['copy false']);
});

it('warms one material for a stretched mesh and a whole one alike, as each takes its own shader', () => {
  const faded = new Palette(scene).toon({ texture: null, color: Color3.White() }, 0.5);
  const [whole, stretched] = ['whole', 'stretched'].map((name) => {
    const box = CreateBox(name, {}, scene);
    box.material = faded;
    return box;
  });
  stretched!.scaling.set(1, 1.5, 1);
  expect(
    warmedFor([
      [faded, whole!],
      [faded, stretched!],
    ]),
  ).toEqual(['stretched true', 'whole false']);
});
