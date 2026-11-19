import { readFileSync } from 'node:fs';
import path from 'node:path';

import { TargetCamera } from '@babylonjs/core/Cameras/targetCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { Scene } from '@babylonjs/core/scene';
import { afterAll, beforeAll, expect, it } from 'vitest';

import { servePublic } from '@/testing/public-on-disk';
import { builtShaders } from '@/testing/shaders';

import { Body, STILL, sunShadows } from '../world3d/body';
import { classLook } from '../world3d/looks';
import { loadMap } from '../world3d/map';
import { Models } from '../world3d/models';
import { Palette } from '../world3d/toon';

import { warmAfterimage } from './afterimage';
import { Fx } from './fx';
import { emptyManifest } from './manifest';

const FRAME_MS = 16;

let engine: NullEngine;
let scene: Scene;

beforeAll(() => {
  servePublic();
  engine = new NullEngine();
  scene = new Scene(engine);
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

// combat-effects.md section 6: the copy is drawn through a depth pre-pass, whose shader differs from the colour pass's, posed as the figure stands at the cut.
it('warms a figure’s afterimage for both of its passes and every pose, so no cancel through an attack or a skill builds a shader', async () => {
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  const palette = new Palette(scene);
  const look = classLook('magician');
  const container = await new Models(
    scene,
    (f) => new Uint8Array(readFileSync(path.resolve(process.cwd(), 'public/models', f))),
  ).load(look.file);
  const figure = new Body(scene, container, look, { palette, shadows: sunShadows(sun, 256) });
  const camera = new TargetCamera('eye', new Vector3(0, 1, -4), scene);
  camera.setTarget(new Vector3(0, 1, 0));
  let now = 1000;
  const fx = new Fx(
    scene,
    await loadMap('forest'),
    { manifest: emptyManifest, sound: () => undefined },
    () => now,
    palette,
  );
  const frame = () => {
    now += FRAME_MS;
    fx.update(now);
    figure.update(now, FRAME_MS, STILL, true);
    scene.render();
  };
  for (let i = 0; i < 30; i++) {
    frame();
  }
  warmAfterimage(fx, figure);
  frame();
  const built = new Set(builtShaders(engine));
  const copy = scene.getTransformNodeByName(`${look.file} still`)!;
  const stretched = new Set<string>(); // a copy's mesh drawn stretched, which a pose's hair of stray scale would switch to a shader of its own
  for (const kind of ['attack', 'skill'] as const) {
    const start = now;
    figure.act(kind, {
      start,
      hit: start + 300,
      strikeEnd: start + 400,
      readyAt: start + 700,
      end: start + 800,
    });
    for (let ms = 0; ms < 800; ms += 4 * FRAME_MS) {
      fx.play('cancel.afterimage', { x: 0, y: 0, z: 0, yaw: 0, copy: figure });
      for (let i = 0; i < 4; i++) {
        frame();
      }
      copy.getChildMeshes(false).forEach((m) => m.nonUniformScaling && stretched.add(m.name));
    }
  }
  expect([...built].some((k) => k.includes('DEPTHPREPASS'))).toBe(true); // the pre-pass drew, its shader among the warmed
  expect(builtShaders(engine).filter((k) => !built.has(k))).toEqual([]);
  expect([...stretched]).toEqual([]);
});
