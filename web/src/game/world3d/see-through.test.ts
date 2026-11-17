import { FreeCamera } from '@babylonjs/core/Cameras/freeCamera';
import { Constants } from '@babylonjs/core/Engines/constants';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { behindPlace, ghostOf, onPlace } from './see-through';
import { Palette } from './toon';

let engine: NullEngine;
let scene: Scene;
let palette: Palette;

beforeEach(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  new FreeCamera('camera', Vector3.Zero(), scene);
  palette = new Palette(scene);
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

const base = { texture: null, color: new Color3(0.5, 0.5, 0.5) };
const inkOf = (m: Mesh) => m.getChildMeshes().find((c) => c.name.endsWith(' ink'))!.material!;

describe('drawing what the place hides', () => {
  it('has every mesh with no stencil of its own mark it where drawn nearest, each frame, whatever turned it off', () => {
    engine.setStencilBuffer(false);
    engine.setStencilFunctionReference(6); // as the lock's outline leaves it round its mob
    scene.render();
    expect([
      engine.getStencilBuffer(),
      engine.getStencilFunction(),
      engine.getStencilFunctionReference(),
      engine.getStencilOperationPass(),
    ]).toEqual([true, Constants.ALWAYS, 1, Constants.REPLACE]);
  });

  it('the place and its ink clear the mark where drawn nearest; a body and its ink take the scene’s', () => {
    const place = CreateBox('place', {}, scene);
    place.material = palette.toon(base, 1, true);
    palette.outline(place, true);
    const body = CreateBox('body', {}, scene);
    body.material = palette.toon(base);
    palette.outline(body);
    for (const m of [place.material, inkOf(place)]) {
      expect([
        m.stencil.enabled,
        m.stencil.func,
        m.stencil.funcRef,
        m.stencil.opStencilDepthPass,
      ]).toEqual([true, Constants.ALWAYS, 0, Constants.REPLACE]);
    }
    expect([body.material.stencil.enabled, inkOf(body).stencil.enabled]).toEqual([false, false]);
  });

  it('a ghost draws only where something nearer cleared the mark, a decal only where the place under it did, and neither changes it', () => {
    const ghost = new StandardMaterial('ghost', scene);
    behindPlace(ghost);
    expect([
      ghost.depthFunction,
      ghost.disableDepthWrite,
      ghost.stencil.enabled,
      ghost.stencil.func,
      ghost.stencil.funcRef,
      ghost.stencil.opStencilDepthPass,
    ]).toEqual([Constants.GREATER, true, true, Constants.EQUAL, 0, Constants.KEEP]);
    const decal = new StandardMaterial('decal', scene);
    onPlace(decal);
    expect([
      decal.depthFunction,
      decal.stencil.enabled,
      decal.stencil.func,
      decal.stencil.funcRef,
      decal.stencil.opStencilDepthPass,
    ]).toEqual([0, true, Constants.EQUAL, 0, Constants.KEEP]); // the scene's depth test, on what the place cleared
  });

  it('a ghost shares its mesh’s geometry and group, and moves, hides and goes with it', () => {
    const mesh = CreateBox('telegraph', {}, scene);
    mesh.renderingGroupId = 1;
    mesh.position.set(3, 0, 4);
    const ghost = ghostOf(mesh, new StandardMaterial('ghost', scene));
    expect([ghost.name, ghost.parent, ghost.geometry, ghost.renderingGroupId]).toEqual([
      'telegraph ghost',
      mesh,
      mesh.geometry,
      1,
    ]);
    expect(ghost.computeWorldMatrix(true).getTranslation().asArray()).toEqual([3, 0, 4]);
    mesh.setEnabled(false);
    expect(ghost.isEnabled()).toBe(false);
    mesh.dispose();
    expect(ghost.isDisposed()).toBe(true);
  });
});
