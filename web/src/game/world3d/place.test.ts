import { readFileSync } from 'node:fs';
import path from 'node:path';

import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import type { Material } from '@babylonjs/core/Materials/material';
import type { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { height } from '@/sim/terrain';

import type { Lighting } from './body';
import { groundUnder, loadMap } from './map';
import type { WorldMap } from './map';
import { Models } from './models';
import type { ModelSource } from './models';
import { buildPlace, fade } from './place';
import { linearOf, Palette } from './toon';

const MAPS = path.resolve(process.cwd(), 'public/maps');
const MODELS = path.resolve(process.cwd(), 'public/models');

const mapOf = (id: string): Promise<WorldMap> =>
  loadMap(id, (m) =>
    Promise.resolve(JSON.parse(readFileSync(path.join(MAPS, `${m}.json`), 'utf8')) as unknown),
  );

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
let loaded: string[];
const source: ModelSource = (file) => {
  loaded.push(file);
  return new Uint8Array(readFileSync(path.join(MODELS, file)));
};

beforeEach(() => {
  engine = new NullEngine();
  engine.getCaps().instancedArrays = true; // as WebGL2 has it; the null engine says no
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: new ShadowGenerator(256, sun) };
  loaded = [];
});

afterEach(() => {
  scene.dispose();
  engine.dispose();
});

/** The meshes drawn, but for the ink. */
const drawn = () => scene.meshes.filter((m) => m.isEnabled() && !m.name.endsWith(' ink'));

/** m once its build, which loads its blocks' code first, is done. */
async function built(m: Material | null): Promise<NodeMaterial> {
  const n = m as NodeMaterial;
  while (n.buildIsInProgress) {
    await new Promise((r) => setTimeout(r, 1));
  }
  return n;
}

function inked(m: AbstractMesh): boolean {
  return m
    .getChildMeshes(true)
    .some((c) => c.name === `${m.name} ink` && c.material?.name === 'ink');
}

describe('a map drawn', () => {
  it('stands each solid on its collision shape, toon-drawn and inked', async () => {
    for (const id of ['town', 'forest']) {
      const map = await mapOf(id);
      const place = await buildPlace(scene, map, lighting, new Models(scene, source));
      expect(place.solids).toHaveLength(map.solids.length);
      map.solids.forEach((s, i) => {
        const at = s.kind === 'box' ? s.box : s.cylinder;
        const look = place.solids[i]!;
        expect(at.x).toBeGreaterThan(look.min.x);
        expect(at.x).toBeLessThan(look.max.x);
        expect(at.z).toBeGreaterThan(look.min.z);
        expect(at.z).toBeLessThan(look.max.z);
        expect(look.max.y).toBeCloseTo(at.top, 6);
        expect(look.min.y).toBeLessThan(height(map.geometry.terrain, at.x, at.z));
      });
      for (const s of map.solids) {
        const m = drawn().find((d) => d.name === s.model)!;
        expect(m.material?.name).toMatch(/^toon /);
        expect(inked(m)).toBe(true);
      }
      expect([place.sizeX, place.sizeZ]).toEqual([map.geometry.size_x, map.geometry.size_z]);
      place.dispose();
    }
  });

  it('turns a box’s stand-in so its width runs along the box’s axis', async () => {
    const map = await mapOf('town');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    for (const s of map.solids) {
      if (s.kind !== 'box') {
        continue;
      }
      const m = drawn().find((d) => {
        const at = d.getAbsolutePosition();
        return (
          d.name === s.model && Math.abs(at.x - s.box.x) < 1e-6 && Math.abs(at.z - s.box.z) < 1e-6
        );
      })!;
      const across = Vector3.TransformNormal(Vector3.Right(), m.computeWorldMatrix(true));
      expect(across.x).toBeCloseTo(s.box.ux, 6); // the world matrix is single precision
      expect(across.z).toBeCloseTo(s.box.uz, 6);
      expect(across.y).toBeCloseTo(0, 6);
    }
  });

  it('lays the ground on the height grid, lit from above, in its key’s colour', async () => {
    const map = await mapOf('forest');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    const ground = scene.getMeshByName('ground') as Mesh;
    const t = map.geometry.terrain;
    expect(ground.getTotalVertices()).toBe(t.nx * t.nz);
    const ys = ground.getVerticesData(VertexBuffer.PositionKind)!.filter((_, i) => i % 3 === 1);
    expect(ys).toEqual(t.heights);
    const normals = ground.getVerticesData(VertexBuffer.NormalKind)!;
    for (let i = 1; i < normals.length; i += 3) {
      expect(normals[i]).toBeGreaterThan(0);
    }
    expect(ground.receiveShadows).toBe(true);
    expect(ground.material?.name).toBe(`toon - ${linearOf(0x8cc265).toHexString()}`);
  });

  it('glows each portal on its ground, and strews the props', async () => {
    const map = await mapOf('forest');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    for (const p of map.portals) {
      const disc = scene.getMeshByName(`portal ${p.name}`)!;
      expect([disc.position.x, disc.position.z]).toEqual([p.x, p.z]);
      expect(disc.position.y).toBeCloseTo(groundUnder(map, p.x, p.z) + 0.03, 9);
      expect((await built(disc.material)).needAlphaBlending()).toBe(true);
    }
    expect((scene.getMeshByName('prop flowers') as Mesh).thinInstanceCount).toBe(120);
    expect((scene.getMeshByName('prop grass_tuft') as Mesh).thinInstanceCount).toBe(1);
  });

  it('colours the sky and fogs it by its key, and a dark cave closes in', async () => {
    const forest = await buildPlace(
      scene,
      await mapOf('forest'),
      lighting,
      new Models(scene, source),
    );
    expect(scene.clearColor.toHexString().slice(0, 7).toLowerCase()).toBe('#9ecbf5');
    expect(scene.fogMode).toBe(Scene.FOGMODE_LINEAR);
    forest.dispose();
    expect(scene.getMeshByName('ground')).toBeNull();
    expect(scene.fogMode).toBe(Scene.FOGMODE_NONE);
    await buildPlace(scene, await mapOf('cave'), lighting, new Models(scene, source));
    expect(scene.fogEnd).toBe(42);
    expect(scene.clearColor.r).toBeLessThan(0.1);
  });
});

describe('kit models', () => {
  const mushnub = { file: 'quaternius/Mushnub.glb', scale: 2, y: 0.1 };

  it('draw a solid or prop whose key has one: turned, scaled, raised, toon-drawn and inked, each file loaded once', async () => {
    const map = await mapOf('forest');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source), {
      tree_oak: mushnub,
      grass_tuft: mushnub,
    });
    const oaks = map.solids.filter((s) => s.model === 'tree_oak');
    const kits = scene.transformNodes.filter((n) => n.name === 'kit tree_oak');
    expect(kits).toHaveLength(oaks.length);
    oaks.forEach((s, i) => {
      const at = s.kind === 'cylinder' ? s.cylinder : null;
      const k = kits[i]!;
      expect([k.position.x, k.position.z]).toEqual([at?.x, at?.z]);
      expect(k.position.y).toBeCloseTo(height(map.geometry.terrain, at!.x, at!.z) + 0.1, 9);
      expect(k.scaling.x).toBe(2);
      const meshes = k
        .getChildMeshes(false)
        .filter((m) => m.getTotalVertices() > 0 && !m.name.endsWith(' ink'));
      expect(meshes.length).toBeGreaterThan(0);
      for (const m of meshes) {
        expect(m.material?.name).toMatch(/^toon /);
        expect(inked(m)).toBe(true);
      }
    });
    expect(drawn().some((m) => m.name === 'tree_oak')).toBe(false); // no stand-in where there's a model
    expect(scene.transformNodes.filter((n) => n.name === 'kit grass_tuft')).toHaveLength(1);
    expect(scene.getMeshByName('prop grass_tuft')).toBeNull();
    expect(scene.getMeshByName('tree_pine')).not.toBeNull(); // a key with no model keeps its stand-in
    expect(loaded).toEqual(['quaternius/Mushnub.glb']);
    const oak = place.solids[map.solids.indexOf(oaks[0]!)]!;
    expect(oak.max.y).toBeGreaterThan(oak.min.y);
  });
});

describe('fade', () => {
  /** The meshes drawn faded now. */
  const faded = () => drawn().filter((m) => m.material?.name.endsWith(' 0.3'));

  it('fades a solid between the camera and the head to 30% without its ink, and brings it back', async () => {
    const map = await mapOf('town');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const house = place.solids[0]!;
    const mid = house.min.add(house.max).scale(0.5);
    const up = new Vector3(0, 10, -10);
    fade(mid.add(up), mid.subtract(up), place.solids);
    const model = map.solids[0]!.model;
    expect(faded().map((m) => m.name)).toEqual([model, `${model} roof`]);
    const body = faded()[0]!;
    expect((await built(body.material)).needAlphaBlending()).toBe(true);
    expect(
      body
        .getChildMeshes(true)
        .find((c) => c.name.endsWith(' ink'))
        ?.isEnabled(),
    ).toBe(false);

    fade(new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), place.solids);
    expect(faded()).toEqual([]);
    expect((await built(body.material)).needAlphaBlending()).toBe(false);
    expect(
      body
        .getChildMeshes(true)
        .find((c) => c.name.endsWith(' ink'))
        ?.isEnabled(),
    ).toBe(true);
  });

  it('leaves a solid beside the camera’s line alone, and one past the head', async () => {
    const place = await buildPlace(scene, await mapOf('town'), lighting, new Models(scene, source));
    const s = place.solids[0]!;
    const mid = s.min.add(s.max).scale(0.5);
    const beside = new Vector3(s.max.x - s.min.x + 1, 0, 0);
    fade(mid.add(beside).add(new Vector3(0, 10, -10)), mid.add(beside), [s]);
    expect(faded()).toEqual([]);
    const north = new Vector3(0, 0, s.max.z - mid.z + 1);
    fade(mid.add(north).add(new Vector3(0, 10, 10)), mid.add(north), [s]); // the head stands past it, the camera beyond
    expect(faded()).toEqual([]);
  });
});
