import { readFileSync } from 'node:fs';
import path from 'node:path';

import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { builtShaders } from '@/testing/shaders';

import { sunShadows } from './body';
import type { Lighting } from './body';
import { Figures } from './figures';
import { mobLook } from './looks';
import { Models } from './models';
import { Outline } from './outline';
import { Palette } from './toon';

const MODELS = path.resolve(process.cwd(), 'public/models');

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;

beforeEach(() => {
  engine = new NullEngine();
  engine.getCaps().instancedArrays = true; // as WebGL2 has it; the null engine says no
  scene = new Scene(engine);
  new TargetCamera('camera', new Vector3(0, 3, -6), scene).setTarget(Vector3.Zero());
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

describe('Outline', () => {
  it('builds its shaders ahead for each mesh new to the scene, as it draws them all, so the first lock builds none: a mob’s skinned body and a prop’s thin instances', async () => {
    const models = new Models(
      scene,
      (file) => new Uint8Array(readFileSync(path.join(MODELS, file))),
    );
    const mob = await new Figures(scene, lighting, models).make(mobLook('green_slime'));
    const prop = CreateBox('prop', {}, scene);
    prop.material = lighting.palette.toon({ texture: null, color: Color3.Gray() });
    prop.thinInstanceAdd([Matrix.Translation(2, 0, 0), Matrix.Translation(-2, 0, 0)]);
    const outline = new Outline(scene);
    for (let i = 0; i < 5; i++) {
      outline.ready();
      scene.render();
      await new Promise((r) => setTimeout(r, 0)); // its shader source loads
    }
    const built = new Set(builtShaders(engine));
    outline.mark(mob);
    scene.render();
    expect([
      builtShaders(engine).some((k) => k.startsWith('glowMapGeneration')),
      builtShaders(engine).filter((k) => !built.has(k)),
    ]).toEqual([true, []]);
  });
});
