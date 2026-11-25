import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { LightBlock } from '@babylonjs/core/Materials/Node/Blocks/Dual/lightBlock';
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

it('draws a ground in its vertices’ colours, in a material of its own', () => {
  const palette = new Palette(scene);
  const plain = palette.toon({ texture: null, color: Color3.White() });
  const ground = palette.toon({ texture: null, color: Color3.White(), vertexColors: true });
  expect(ground).not.toBe(plain);
  expect(palette.toon({ texture: null, color: Color3.White(), vertexColors: true })).toBe(ground);
  expect(ground.getBlockByName('vertexColor')).not.toBeNull();
  expect(plain.getBlockByName('vertexColor')).toBeNull();
});

// From a live test: a slime in a round bush showed in a black box, the far side of the bush's ink through the hole the place opened, with the bush's own back faces culled.
it('makes a toon hollow only while whole and cut: both faces drawn, the back ones gone but where an opening shows them', () => {
  const palette = new Palette(scene);
  const base = { texture: null, color: new Color3(0.2, 0.5, 0.2) };
  const hollow = palette.toon(base, 1, true, true);
  expect(hollow.name).toMatch(/ cut hollow$/);
  expect(hollow.backFaceCulling).toBe(false);
  expect(hollow.getBlockByName('cutBack')).not.toBeNull();
  for (const [m, why] of [
    [palette.toon(base, 0.3, true, true), 'faded'],
    [palette.toon(base, 1, false, true), 'uncut'],
    [palette.toon(base, 1, true), 'not hollow'],
  ] as const) {
    expect(m.name, why).not.toMatch(/hollow/);
    expect(m.backFaceCulling, why).toBe(true);
    expect(m.getBlockByName('cutBack'), why).toBeNull();
  }
});

// A region's sun casts cascaded shadows on WebGL2, whose receivers pick a cascade by view depth: a light without the view matrix fails to compile there, and a null engine compiles nothing, so the graph is checked.
it('gives every light in a toon material, land or sea, the view matrix cascaded shadows need', () => {
  for (const palette of [new Palette(scene), new Palette(scene, 'sea')]) {
    const base = { texture: null, color: Color3.White() };
    palette.toon(base);
    palette.toon(base, 0.5);
    palette.toon(base, 1, true);
    palette.toon(base, 1, true, true);
    palette.toon({ ...base, vertexColors: true });
    palette.toon({ ...base, glow: true });
    palette.toon(base, 1, false, false, 3);
    palette.apron(base);
  }
  const lit = scene.materials.filter(
    (m): m is NodeMaterial => m instanceof NodeMaterial && !m.name.includes(' glow'), // a glow is unlit
  );
  expect(lit).toHaveLength(14);
  for (const m of lit) {
    const lights = m.attachedBlocks.filter((b): b is LightBlock => b instanceof LightBlock);
    expect(lights).toHaveLength(1);
    expect(lights[0]!.view.isConnected).toBe(true);
  }
});
