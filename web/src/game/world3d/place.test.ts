import { readFileSync } from 'node:fs';
import path from 'node:path';

import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import type { Material } from '@babylonjs/core/Materials/material';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Quaternion, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { Scene } from '@babylonjs/core/scene';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { height } from '@/sim/terrain';
import { servePublic } from '@/testing/public-on-disk';

import type { Lighting } from './body';
import { cameraAt } from './follow';
import type { KitModel } from './kit-models';
import { groundUnder, loadMap } from './map';
import { Models } from './models';
import type { ModelSource } from './models';
import { buildPlace, fade } from './place';
import { linearOf, Palette } from './toon';

const kits: Record<string, KitModel> = vi.hoisted(() => ({}));
vi.mock('./kit-models', () => ({ kitModels: kits }));

const MODELS = path.resolve(process.cwd(), 'public/models');

const mapOf = (id: string) => loadMap(id);

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
let loaded: string[];
const source: ModelSource = (file) => {
  loaded.push(file);
  return new Uint8Array(readFileSync(path.join(MODELS, file)));
};

beforeEach(() => {
  servePublic();
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
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
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

  it('draws the cells too steep to walk, the mesa’s edge, darker on the same vertices', async () => {
    const map = await mapOf('forest');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    const ground = scene.getMeshByName('ground') as Mesh;
    const cliffs = scene.getMeshByName('cliffs') as Mesh;
    const t = map.geometry.terrain;
    const steep = t.steepX.filter((s, k) => s || t.steepZ[k]).length;
    expect(steep).toBeGreaterThan(0);
    expect(cliffs.getTotalIndices()).toBe(6 * steep);
    expect(ground.getTotalIndices() + cliffs.getTotalIndices()).toBe(6 * (t.nx - 1) * (t.nz - 1));
    expect(cliffs.getVerticesData(VertexBuffer.NormalKind)).toEqual(
      ground.getVerticesData(VertexBuffer.NormalKind),
    );
    expect(cliffs.receiveShadows).toBe(true);
    expect(cliffs.material?.name).toBe(`toon - ${linearOf(0x8cc265).scale(0.6).toHexString()}`);
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
    expect(scene.getMeshByName('cliffs')).toBeNull();
    expect(scene.fogMode).toBe(Scene.FOGMODE_NONE);
    await buildPlace(scene, await mapOf('cave'), lighting, new Models(scene, source));
    expect(scene.fogEnd).toBe(42);
    expect(scene.clearColor.r).toBeLessThan(0.1);
  });
});

describe('kit models', () => {
  const mushnub: KitModel = { file: 'quaternius/Mushnub.glb', scale: 2, y: 0.1 };

  beforeEach(() => {
    kits.tree_oak = mushnub;
    kits.grass_tuft = mushnub;
  });

  afterEach(() => {
    for (const key of Object.keys(kits)) {
      delete kits[key];
    }
  });

  /** The meshes key's model draws its placements with, but for ink and faded copies. */
  const templates = (key: string) =>
    scene.meshes.filter(
      (m): m is Mesh =>
        m instanceof Mesh &&
        m.name.startsWith(`${key} `) &&
        !m.name.endsWith(' ink') &&
        !m.name.endsWith(' faded'),
    );

  /** The faded copies of key's solids drawn now. */
  const copies = (key: string) =>
    scene.meshes.filter(
      (m) => m.name.startsWith(`${key} `) && m.name.endsWith(' faded') && m.isEnabled(),
    );

  /** The lowest and highest the file's model reaches, skinned in its rest pose as a body draws it. */
  async function restPose(file: string): Promise<{ min: number; max: number }> {
    const made = (await new Models(scene, source).load(file)).instantiateModelsToScene(
      (n) => `posed ${n}`,
      false,
      { doNotInstantiate: true },
    );
    for (const m of made.rootNodes.flatMap((r) => r.getChildMeshes(false))) {
      m.skeleton?.prepare(true);
      m.refreshBoundingInfo({ applySkeleton: true });
    }
    const { min, max } = made.rootNodes[0]!.getHierarchyBoundingVectors(true);
    made.dispose();
    return { min: min.y, max: max.y };
  }

  /** Where a thin instance stands, and its scale. */
  function placed(m: Mesh, i: number): { at: Vector3; scale: Vector3 } {
    const at = new Vector3();
    const scale = new Vector3();
    m.thinInstanceGetWorldMatrices()[i]!.decompose(scale, new Quaternion(), at);
    return { at, scale };
  }

  it('draw every placement of a key as a thin instance of its model, made once: turned, scaled, raised, toon-drawn and inked', async () => {
    const map = await mapOf('forest');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const oaks = map.solids.filter((s) => s.model === 'tree_oak');
    const meshes = templates('tree_oak');
    expect(meshes.length).toBeGreaterThan(0);
    for (const m of meshes) {
      expect(m.thinInstanceCount).toBe(oaks.length);
      expect(m.computeWorldMatrix(true).isIdentity()).toBe(true); // baked into the model's space
      expect(m.skeleton).toBeNull();
      expect(m.material?.name).toMatch(/^toon /);
      expect(inked(m)).toBe(true);
      const ink = m.getChildMeshes(true).find((c) => c.name === `${m.name} ink`) as Mesh;
      expect(ink.thinInstanceCount).toBe(oaks.length);
      oaks.forEach((s, i) => {
        const at = s.kind === 'cylinder' ? s.cylinder : s.box;
        const p = placed(m, i);
        expect([p.at.x, p.at.z]).toEqual([expect.closeTo(at.x, 4), expect.closeTo(at.z, 4)]);
        expect(p.at.y).toBeCloseTo(height(map.geometry.terrain, at.x, at.z) + 0.1, 4);
        expect(p.scale.x).toBeCloseTo(2, 5);
      });
    }
    for (const m of templates('grass_tuft')) {
      expect(m.thinInstanceCount).toBe(map.props.filter((p) => p.model === 'grass_tuft').length);
    }
    expect(drawn().some((m) => m.name === 'tree_oak')).toBe(false); // no stand-in where there's a model
    expect(scene.getMeshByName('prop grass_tuft')).toBeNull();
    expect(scene.getMeshByName('tree_pine')).not.toBeNull(); // a key with no model keeps its stand-in
    expect(loaded).toEqual(['quaternius/Mushnub.glb']);
    expect([scene.skeletons, scene.animationGroups]).toEqual([[], []]); // a kit model stands still
    const oak = place.solids[map.solids.indexOf(oaks[0]!)]!;
    const at = oaks[0]!.kind === 'cylinder' ? oaks[0]!.cylinder : oaks[0]!.box;
    const posed = await restPose('quaternius/Mushnub.glb');
    expect(oak.min.y).toBeCloseTo(
      height(map.geometry.terrain, at.x, at.z) + 0.1 + 2 * posed.min,
      4,
    );
    expect(oak.max.y - oak.min.y).toBeCloseTo(2 * (posed.max - posed.min), 4);
    expect([oak.min.x < at.x, at.x < oak.max.x, oak.min.z < at.z, at.z < oak.max.z]).toEqual([
      true,
      true,
      true,
      true,
    ]);
  });

  it('fade a solid by hiding its instance, ink and all, and showing a faded copy of it alone; then bring it back', async () => {
    const map = await mapOf('forest');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const first = map.solids.findIndex((s) => s.model === 'tree_oak');
    const oak = place.solids[first]!;
    const meshes = templates('tree_oak');
    const inks = meshes.map(
      (m) => m.getChildMeshes(true).find((c) => c.name.endsWith(' ink')) as Mesh,
    );
    const mid = oak.min.add(oak.max).scale(0.5);
    const up = new Vector3(0, 10, -10);
    expect(copies('tree_oak')).toEqual([]);

    fade(mid.add(up), mid.subtract(up), place.solids);
    expect(copies('tree_oak')).toHaveLength(meshes.length);
    const s = map.solids[first]!;
    const at = s.kind === 'cylinder' ? s.cylinder : s.box;
    for (const c of copies('tree_oak')) {
      expect(c.material?.name).toMatch(/ 0\.3$/);
      expect([c.position.x, c.position.z]).toEqual([
        expect.closeTo(at.x, 9),
        expect.closeTo(at.z, 9),
      ]);
      expect(c.scaling.x).toBeCloseTo(2, 9);
    }
    for (const m of [...meshes, ...inks]) {
      expect(placed(m, 0).scale.x).toBe(0);
      expect(placed(m, 1).scale.x).toBeCloseTo(2, 5); // the other oaks stand
    }

    fade(new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), place.solids);
    expect(copies('tree_oak')).toEqual([]);
    for (const m of [...meshes, ...inks]) {
      expect(placed(m, 0).scale.x).toBeCloseTo(2, 5);
    }
  });

  it.each([
    ['crossroads', 9, 20.5, 11, 8], // in the south house, on the road in from the heights
    ['crossroads', 13, 20.5, 11, 8],
    ['crossroads', 17, 20, 11, 8], // past it, at its roof's edge
    ['crossroads', 12, 37, 12, 31], // the inn
    ['town', 40, 29, 39, 24], // the inn
  ])(
    'take from the view a building the camera is in or at, its shadow kept: %s, a player at (%d, %d)',
    async (id, x, z, bx, bz) => {
      const { kitModels } = await vi.importActual<{
        kitModels: Readonly<Record<string, KitModel>>;
      }>('./kit-models');
      Object.assign(kits, kitModels);
      const map = await mapOf(id);
      const place = await buildPlace(scene, map, lighting, new Models(scene, source));
      const k = map.solids.findIndex((s) => s.kind === 'box' && s.box.x === bx && s.box.z === bz);
      const s = map.solids[k]!;
      const look = place.solids[k]!;
      const y = groundUnder(map, x, z);
      const camera = cameraAt(x, y, z, new Vector3());
      const out = (a: 'x' | 'y' | 'z') =>
        Math.max(look.min[a] - camera[a], 0, camera[a] - look.max[a]);
      expect(Math.hypot(out('x'), out('y'), out('z'))).toBeLessThan(2); // where the tour found the camera

      fade(camera, new Vector3(x, y + 1.75, z), place.solids);
      const gone = copies(s.model).filter(
        (c) => Math.hypot(c.position.x - bx, c.position.z - bz) < 1e-6,
      );
      expect(gone).toHaveLength(templates(s.model).length);
      for (const c of gone) {
        expect(c.material?.name).toMatch(/ 0$/);
        expect(lighting.shadows.getShadowMap()?.renderList).toContain(c);
      }
      const n = map.solids.slice(0, k).filter((t) => t.model === s.model).length;
      for (const m of templates(s.model)) {
        expect(placed(m, n).scale.x).toBe(0);
      }

      fade(new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), place.solids);
      expect(copies(s.model)).toEqual([]);
    },
  );

  it.each(['town', 'forest'])(
    'draw %s from the real kit models, each key with one drawn from its file and no stand-in',
    async (id) => {
      const { kitModels } = await vi.importActual<{
        kitModels: Readonly<Record<string, KitModel>>;
      }>('./kit-models');
      Object.assign(kits, kitModels);
      const map = await mapOf(id);
      await buildPlace(scene, map, lighting, new Models(scene, source));
      const keys = [...new Set([...map.solids, ...map.props].map((s) => s.model))].filter(
        (k) => k in kitModels,
      );
      expect(keys).not.toEqual([]);
      for (const key of keys) {
        expect(templates(key), key).not.toEqual([]);
        expect([scene.getMeshByName(key), scene.getMeshByName(`prop ${key}`)], key).toEqual([
          null,
          null,
        ]);
      }
      expect(loaded.sort()).toEqual([...new Set(keys.map((k) => kitModels[k]!.file))].sort());
    },
  );

  it('take all they made with the place', async () => {
    const counts = () =>
      [scene.meshes, scene.transformNodes, scene.skeletons, scene.animationGroups].map(
        (a) => a.length,
      );
    const before = counts();
    const place = await buildPlace(
      scene,
      await mapOf('forest'),
      lighting,
      new Models(scene, source),
    );
    place.dispose();
    expect(counts()).toEqual(before);
  });
});

describe('warming', () => {
  it('builds each faded and gone material’s shaders as the place is drawn, on a submesh of its own, so a first fade doesn’t stall', async () => {
    kits.tree_oak = { file: 'quaternius/Mushnub.glb', scale: 2, y: 0.1 };
    const ready = vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh');
    await buildPlace(scene, await mapOf('forest'), lighting, new Models(scene, source));
    delete kits.tree_oak;
    const faded = scene.materials.filter((m) => / 0(\.3)?$/.test(m.name));
    expect(faded.filter((m) => m.name.endsWith(' 0')).length).toBeGreaterThan(1);
    expect(faded.filter((m) => m.name.endsWith(' 0.3')).length).toBeGreaterThan(1);
    expect(new Set(ready.mock.contexts)).toEqual(new Set(faded));
    for (const [mesh, sub] of ready.mock.calls) {
      expect(mesh.subMeshes).not.toContain(sub);
    }
    await vi.waitFor(() => {
      for (const m of faded) {
        expect((m as NodeMaterial).buildIsInProgress).toBe(false);
      }
    });
  });

  it('stops warming once the place is gone', async () => {
    const ready = vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh').mockReturnValue(false);
    const place = await buildPlace(scene, await mapOf('town'), lighting, new Models(scene, source));
    place.dispose();
    const calls = ready.mock.calls.length;
    await new Promise((r) => setTimeout(r, 60));
    expect(ready.mock.calls.length).toBe(calls);
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

  it('takes a solid from the view, ink and all, when the camera is in it or within 2 m; its shadow kept', async () => {
    const map = await mapOf('town');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const house = place.solids[0]!;
    const model = map.solids[0]!.model;
    const mid = house.min.add(house.max).scale(0.5);
    const gone = () => drawn().filter((m) => m.material?.name.endsWith(' 0'));
    const east = (d: number) => new Vector3(house.max.x + d, mid.y, mid.z); // beside it, the head further east
    const ink = (m: AbstractMesh) => m.getChildMeshes(true).find((c) => c.name.endsWith(' ink'));

    fade(mid, mid.add(new Vector3(0, -10, 0)), place.solids);
    expect(gone().map((m) => m.name)).toEqual([model, `${model} roof`]);
    for (const m of gone()) {
      expect(ink(m)?.isEnabled()).toBe(false);
      expect(lighting.shadows.getShadowMap()?.renderList).toContain(m);
    }
    fade(east(1.9), east(10), place.solids);
    expect(gone().map((m) => m.name)).toEqual([model, `${model} roof`]);

    fade(east(2.1), east(10), place.solids);
    expect(gone()).toEqual([]);
    expect(faded()).toEqual([]);
    expect(ink(scene.getMeshByName(model)!)?.isEnabled()).toBe(true);
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
