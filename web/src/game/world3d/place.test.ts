import { readFileSync } from 'node:fs';
import path from 'node:path';

import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { FreeCamera } from '@babylonjs/core/Cameras/freeCamera';
import { NullEngine } from '@babylonjs/core/Engines/nullEngine';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight';
import '@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent';
import type { Material } from '@babylonjs/core/Materials/material';
import type { InputBlock } from '@babylonjs/core/Materials/Node/Blocks/Input/inputBlock';
import { NodeMaterial } from '@babylonjs/core/Materials/Node/nodeMaterial';
import { Matrix, Quaternion, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

import { movement } from '@/gamedata/movement';
import { height } from '@/sim/terrain';
import { servePublic } from '@/testing/public-on-disk';

import { sunShadows } from './body';
import type { Lighting } from './body';
import { CHEST_M, cameraAt, heldView } from './follow';
import type { KitModel } from './kit-models';
import { groundUnder, loadMap } from './map';
import type { Solid, WorldMap } from './map';
import { Models } from './models';
import type { ModelSource } from './models';
import { buildPlace, fade, kitParts, Look, LookGrid, placedBox } from './place';
import type { Place } from './place';
import { linearOf, Palette } from './toon';

const kits: Record<string, KitModel> = vi.hoisted(() => ({}));
vi.mock('./kit-models', () => ({ kitModels: kits }));

const MODELS = path.resolve(process.cwd(), 'public/models');

const mapOf = (id: string) => loadMap(id);

const SETTLED = 1000; // ms in which any look steps all the way to how the camera wants it

/** Steps place's looks over dt as the world does a frame, with the camera at camera and the head at head. */
function fadeIn(place: Place, camera: Vector3, head: Vector3, dt: number): void {
  fade(camera, head, place.looks.around(camera, head), dt);
}

/** Every look on map's place: those round a line across the whole map. */
function allLooks(place: Place, map: WorldMap): Look[] {
  const { size_x: sx, size_z: sz } = map.geometry;
  return place.looks.around(new Vector3(-1, 0, -1), new Vector3(sx + 1, 0, sz + 1));
}

/** The look round solid s: of those whose box holds its centre, the one whose box's middle is nearest it. */
function solidLook(place: Place, s: Solid): Look {
  const at = s.kind === 'box' ? s.box : s.cylinder;
  const off = (l: Look) =>
    Math.hypot((l.min.x + l.max.x) / 2 - at.x, (l.min.z + l.max.z) / 2 - at.z);
  const [found] = place.looks
    .around(new Vector3(at.x, 0, at.z), new Vector3(at.x, 0, at.z))
    .filter((l) => l.min.x < at.x && at.x < l.max.x && l.min.z < at.z && at.z < l.max.z)
    .sort((a, b) => off(a) - off(b));
  expect(found).toBeDefined();
  return found!;
}

let engine: NullEngine;
let scene: Scene;
let lighting: Lighting;
let loaded: string[];
const source: ModelSource = (file) => {
  loaded.push(file);
  return new Uint8Array(readFileSync(path.join(MODELS, file)));
};

let shared = false; // a describe's tests share one scene and what it built

function fresh(): void {
  engine = new NullEngine();
  engine.getCaps().instancedArrays = true; // as WebGL2 has it; the null engine says no
  scene = new Scene(engine);
  const sun = new DirectionalLight('sun', new Vector3(0.35, -1, 0.6).normalize(), scene);
  lighting = { palette: new Palette(scene), shadows: sunShadows(sun, 256) };
  loaded = [];
}

beforeEach(() => {
  servePublic();
  if (!shared) {
    fresh();
  }
});

afterEach(() => {
  if (!shared) {
    scene.dispose();
    engine.dispose();
  }
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** One scene for the describe's tests, made before them and dropped after, so they share what it builds: each only reads it, or gives back what it changes. */
function sharedScene(): void {
  beforeAll(() => {
    servePublic();
    fresh();
    shared = true;
  });
  afterAll(() => {
    shared = false;
    scene.dispose();
    engine.dispose();
  });
}

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

/** Checks the sun's shadow pass draws every one of want, see-through meshes, in one frame; a material not built yet would pass as opaque. */
async function castShadows(want: readonly AbstractMesh[]): Promise<void> {
  for (const m of want) {
    expect((await built(m.material)).needAlphaBlendingForMesh(m), m.name).toBe(true);
  }
  scene.activeCamera ??= new FreeCamera('eye', new Vector3(20, 30, -10), scene);
  const cast = new Set<AbstractMesh>();
  const watch = lighting.shadows!.onBeforeShadowMapRenderMeshObservable.add((m) => cast.add(m));
  try {
    await vi.waitFor(() => {
      cast.clear();
      scene.render();
      expect(want.filter((m) => !cast.has(m))).toEqual([]);
    });
  } finally {
    lighting.shadows!.onBeforeShadowMapRenderMeshObservable.remove(watch);
  }
}

/** Where the game's camera sits, and the head it looks for, with a player at (x, z) on map seen at 1280 by 800. */
function viewFrom(map: WorldMap, x: number, z: number): { camera: Vector3; head: Vector3 } {
  const y = groundUnder(map, x, z);
  const g = map.geometry;
  const look = heldView({ x, y: y + CHEST_M, z }, g.size_x, g.size_z, 1280 / 800, 0, new Vector3());
  return {
    camera: cameraAt(look.x, look.y, look.z, 0, new Vector3()),
    head: new Vector3(x, y + movement.height, z),
  };
}

function inked(m: AbstractMesh): boolean {
  return m
    .getChildMeshes(true)
    .some((c) => c.name === `${m.name} ink` && c.material?.name === 'ink cut');
}

describe('a map drawn', () => {
  it('draws each solid hollow while whole, so an opening shows its inside, not its ink', async () => {
    for (const id of ['yard', 'ridge']) {
      const map = await mapOf(id);
      const place = await buildPlace(scene, map, lighting, new Models(scene, source));
      for (const s of map.solids) {
        const m = drawn().find((d) => d.name === s.model)!;
        expect(m.material?.name, s.model).toMatch(/ cut hollow$/);
        expect(m.material?.backFaceCulling, s.model).toBe(false);
      }
      place.dispose();
    }
  });

  it('stands each solid on its collision shape, toon-drawn and inked', async () => {
    for (const id of ['yard', 'ridge']) {
      const map = await mapOf(id);
      const place = await buildPlace(scene, map, lighting, new Models(scene, source));
      expect(allLooks(place, map)).toHaveLength(map.solids.length);
      map.solids.forEach((s) => {
        const at = s.kind === 'box' ? s.box : s.cylinder;
        const look = solidLook(place, s);
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
    const map = await mapOf('yard');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    for (const s of map.solids) {
      if (s.kind !== 'box') {
        continue;
      }
      const m = drawn().find((d) => {
        const at = d.getAbsolutePosition();
        return (
          d.name === s.model && Math.abs(at.x - s.box.x) < 1e-5 && Math.abs(at.z - s.box.z) < 1e-5
        );
      })!;
      const across = Vector3.TransformNormal(Vector3.Right(), m.computeWorldMatrix(true));
      expect(across.x).toBeCloseTo(s.box.ux, 6); // the world matrix is single precision
      expect(across.z).toBeCloseTo(s.box.uz, 6);
      expect(across.y).toBeCloseTo(0, 6);
    }
  });

  describe('the ridge, drawn once', () => {
    sharedScene();
    let map: WorldMap;
    beforeAll(async () => {
      map = await mapOf('ridge');
      await buildPlace(scene, map, lighting, new Models(scene, source));
    });

    it('lays the ground on the height grid, lit from above, in its key’s colour, on an apron that a hole may show, the place’s but never cut', () => {
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
      expect(ground.material?.name).toBe(`toon - ${linearOf(0xeef2f6).toHexString()} cut`);
      const apron = scene.getMeshByName('apron')!.material!;
      expect([apron.name, apron.stencil.enabled, apron.stencil.funcRef]).toEqual([
        `toon apron ${linearOf(0xeef2f6).scale(0.8).toHexString()}`,
        true,
        0,
      ]); // a ghost or a faint decal draws on it
    });

    it('draws the cells too steep to walk, the mesa’s edge, darker on the same vertices', () => {
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
      expect(cliffs.material?.name).toBe(
        `toon - ${linearOf(0xeef2f6).scale(0.6).toHexString()} cut`,
      );
    });

    it('glows each portal on its ground, and strews the props', async () => {
      for (const p of map.portals) {
        const disc = scene.getMeshByName(`portal ${p.name}`)!;
        expect([disc.position.x, disc.position.z]).toEqual([p.x, p.z]);
        expect(disc.position.y).toBeCloseTo(groundUnder(map, p.x, p.z) + 0.03, 9);
        expect((await built(disc.material)).needAlphaBlending()).toBe(true);
      }
      expect((scene.getMeshByName('prop pebbles') as Mesh).thinInstanceCount).toBe(40);
      expect((scene.getMeshByName('prop tree_pine') as Mesh).thinInstanceCount).toBe(21);
    });
  });

  it('colours the sky and fogs it by its key, and a dark cave closes in', async () => {
    const forest = await buildPlace(
      scene,
      await mapOf('ridge'),
      lighting,
      new Models(scene, source),
    );
    expect(scene.clearColor.toHexString().slice(0, 7).toLowerCase()).toBe('#d9e5ef');
    expect(scene.fogMode).toBe(Scene.FOGMODE_LINEAR);
    forest.dispose();
    expect(scene.getMeshByName('ground')).toBeNull();
    expect(scene.getMeshByName('cliffs')).toBeNull();
    expect(scene.fogMode).toBe(Scene.FOGMODE_NONE);
    await buildPlace(scene, await mapOf('greyfang_cave'), lighting, new Models(scene, source));
    expect([scene.fogStart, scene.fogEnd]).toEqual([14, 40]);
    expect(scene.clearColor.toHexString().slice(0, 7)).toBe('#000000');
  });
});

describe('kit models', () => {
  const mushnub: KitModel = { file: 'quaternius/Mushnub.glb', scale: 2, y: 0.1 };

  beforeEach(() => {
    kits.tree_oak = mushnub;
    kits.fountain_flowers = mushnub;
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
    const map = await mapOf('yard');
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
    for (const m of templates('fountain_flowers')) {
      expect(m.thinInstanceCount).toBe(
        map.props.filter((p) => p.model === 'fountain_flowers').length,
      );
    }
    expect(drawn().some((m) => m.name === 'tree_oak')).toBe(false); // no stand-in where there's a model
    expect(scene.getMeshByName('prop fountain_flowers')).toBeNull();
    expect(scene.getMeshByName('tree_pine')).not.toBeNull(); // a key with no model keeps its stand-in
    expect(loaded).toEqual(['quaternius/Mushnub.glb']);
    expect([scene.skeletons, scene.animationGroups]).toEqual([[], []]); // a kit model stands still
    const oak = solidLook(place, oaks[0]!);
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
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const first = map.solids.findIndex((s) => s.model === 'tree_oak');
    const oak = solidLook(place, map.solids[first]!);
    const meshes = templates('tree_oak');
    const inks = meshes.map(
      (m) => m.getChildMeshes(true).find((c) => c.name.endsWith(' ink')) as Mesh,
    );
    const mid = oak.min.add(oak.max).scale(0.5);
    const up = new Vector3(0, 10, -10);
    expect(copies('tree_oak')).toEqual([]);

    fadeIn(place, mid.add(up), mid.subtract(up), SETTLED);
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
    await castShadows(copies('tree_oak'));

    fadeIn(place, new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), SETTLED);
    expect(copies('tree_oak')).toEqual([]);
    for (const m of [...meshes, ...inks]) {
      expect(placed(m, 0).scale.x).toBeCloseTo(2, 5);
    }
  });

  /** For each spot, a player there puts the game's camera in or at the building at (bx, bz): it leaves the view, its faded copies still cast their shadow, and a camera far off gives it back whole for the next. */
  async function takesFromView(
    map: WorldMap,
    place: Awaited<ReturnType<typeof buildPlace>>,
    spots: readonly (readonly [number, number, number, number])[],
  ): Promise<void> {
    for (const [x, z, bx, bz] of spots) {
      const at = `a player at (${x}, ${z})`;
      const k = map.solids.findIndex((s) => s.kind === 'box' && s.box.x === bx && s.box.z === bz);
      const s = map.solids[k]!;
      const look = solidLook(place, s);
      const { camera, head } = viewFrom(map, x, z);
      const out = (a: 'x' | 'y' | 'z') =>
        Math.max(look.min[a] - camera[a], 0, camera[a] - look.max[a]);
      expect(Math.hypot(out('x'), out('y'), out('z')), at).toBeLessThan(2); // where the tour found the camera

      fadeIn(place, camera, head, SETTLED);
      const gone = copies(s.model).filter(
        (c) => Math.hypot(c.position.x - bx, c.position.z - bz) < 1e-6,
      );
      expect(gone, at).toHaveLength(templates(s.model).length);
      for (const c of gone) {
        expect(c.material?.name, at).toMatch(/ 0$/);
      }
      await castShadows(gone);
      const n = map.solids.slice(0, k).filter((t) => t.model === s.model).length;
      for (const m of templates(s.model)) {
        expect(placed(m, n).scale.x, at).toBe(0);
      }

      fadeIn(place, new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), SETTLED);
      expect(copies(s.model), at).toEqual([]);
      for (const m of templates(s.model)) {
        expect(placed(m, n).scale.x, at).not.toBe(0); // whole again for the next spot
      }
    }
  }

  /** Every key of map's solids and props with a kit model is drawn from its model's file, none as a stand-in, and the files loaded are just theirs. */
  function drawsFromFiles(
    map: WorldMap,
    kitModels: Readonly<Record<string, KitModel>>,
    files: readonly string[],
  ): void {
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
    expect([...files].sort()).toEqual([...new Set(keys.map((k) => kitModels[k]!.file))].sort());
  }

  /** The kit models as the game has them, in place of the stand-ins and the mushnub. */
  async function realKits(): Promise<Readonly<Record<string, KitModel>>> {
    const { kitModels } = await vi.importActual<{
      kitModels: Readonly<Record<string, KitModel>>;
    }>('./kit-models');
    Object.assign(kits, kitModels);
    return kitModels;
  }

  it('take from the view a building the game’s camera is in or at, its shadow still cast, and give it back whole: yard, one place for every spot', async () => {
    await realKits();
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    await takesFromView(map, place, [
      [9, 20.5, 11, 8], // in the south house, on the road in from the heights
      [13, 20.5, 11, 8],
      [17, 20, 11, 8], // past it, at its roof's edge
      [12, 37, 12, 31], // the inn
    ]);
  });

  it('sway a model the water bends, kelp from its file: every fade step and its ink by the model’s height as placed; coral, none', async () => {
    const root = new TransformNode('kits', scene);
    const models = new Models(scene, source);
    const kelp = await kitParts(
      models,
      { file: 'sea/kelp.glb', scale: 1.5, y: 0, sway: true },
      'kelp',
      lighting.palette,
      null,
      root,
    );
    const tall = 1.5 * Math.max(...kelp.map((p) => p.mesh.getBoundingInfo().maximum.y));
    expect(tall).toBeCloseTo(4.5, 0); // the 3 m kelp, scaled
    const swayBy = (m: Material) =>
      ((m as NodeMaterial).getBlockByName('swayTall') as InputBlock | null)?.value as
        number | undefined;
    for (const part of kelp) {
      for (const m of [...part.steps, part.ink!.material!]) {
        expect(swayBy(m)).toBeCloseTo(1 / tall, 9);
      }
    }
    const coral = await kitParts(
      models,
      { file: 'sea/coral_branch.glb', scale: 1, y: 0 },
      'coral',
      lighting.palette,
      null,
      root,
    );
    for (const part of coral) {
      for (const m of [...part.steps, part.ink!.material!]) {
        expect(swayBy(m)).toBeUndefined();
      }
    }
  });

  describe('yard from the real kit models, drawn once', () => {
    sharedScene();
    let map: WorldMap;
    let place: Awaited<ReturnType<typeof buildPlace>>;
    let kitModels: Readonly<Record<string, KitModel>>;
    let files: string[];
    beforeAll(async () => {
      kitModels = await realKits();
      map = await mapOf('yard');
      place = await buildPlace(scene, map, lighting, new Models(scene, source));
      files = [...loaded];
    });

    it('draws each key with one drawn from its file and no stand-in', () => {
      drawsFromFiles(map, kitModels, files);
    });

    it('takes from the view a building the game’s camera is in or at, its shadow still cast, and gives it back whole', async () => {
      await takesFromView(map, place, [[39, 37, 39, 31]]); // the east house
    });
  });

  it('draw the ridge from the real kit models, each key with one drawn from its file and no stand-in', async () => {
    const kitModels = await realKits();
    const map = await mapOf('ridge');
    await buildPlace(scene, map, lighting, new Models(scene, source));
    drawsFromFiles(map, kitModels, loaded);
  });

  it('take all they made with the place', async () => {
    const counts = () =>
      [scene.meshes, scene.transformNodes, scene.skeletons, scene.animationGroups].map(
        (a) => a.length,
      );
    const before = counts();
    const place = await buildPlace(
      scene,
      await mapOf('ridge'),
      lighting,
      new Models(scene, source),
    );
    place.dispose();
    expect(counts()).toEqual(before);
  });
});

describe('warming', () => {
  it('builds each see-through material’s shaders as the place is drawn, on a submesh of its own, so a first fade doesn’t stall', async () => {
    kits.tree_oak = { file: 'quaternius/Mushnub.glb', scale: 2, y: 0.1 };
    const ready = vi.spyOn(NodeMaterial.prototype, 'isReadyForSubMesh');
    await buildPlace(scene, await mapOf('yard'), lighting, new Models(scene, source));
    delete kits.tree_oak;
    const faded = scene.materials.filter((m) => /^toon .* 0(\.\d+)?$/.test(m.name));
    for (const alpha of ['0.65', '0.3', '0.15', '0']) {
      expect(faded.filter((m) => m.name.endsWith(` ${alpha}`)).length, alpha).toBeGreaterThan(1);
    }
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
    const place = await buildPlace(scene, await mapOf('yard'), lighting, new Models(scene, source));
    place.dispose();
    const calls = ready.mock.calls.length;
    await new Promise((r) => setTimeout(r, 60));
    expect(ready.mock.calls.length).toBe(calls);
  });
});

describe('a look grid', () => {
  /** A look round the box from (x0, z0) to (x1, z1), 3 m tall, counting its steps in shown. */
  const box = (x0: number, z0: number, x1: number, z1: number, shown: number[] = []) =>
    new Look(new Vector3(x0, 0, z0), new Vector3(x1, 3, z1), (step) => shown.push(step));
  const at = (x: number, z: number, y = 1) => new Vector3(x, y, z);

  it('files a look under every 8 m cell its box covers, west and south of 0 too, so a line over any of them finds it and one elsewhere doesn’t', () => {
    const grid = new LookGrid();
    const wide = box(-3, -1, 17, 1); // cells -1 to 2 along x, -1 and 0 along z
    grid.add(wide);
    for (const x of [-2.5, 4, 12, 16.5]) {
      expect(grid.around(at(x, -0.5, 10), at(x, -0.5)), `x ${x}`).toEqual([wide]);
    }
    expect(grid.around(at(40, 40, 10), at(40, 30))).toEqual([]);
    expect(grid.around(at(5, 30, 10), at(5, 12))).toEqual([]); // 11 m north of its edge
  });

  it('finds a look within 2 m of the camera though the line stays in the next cell', () => {
    const grid = new LookGrid();
    const near = box(14, 10, 15.9, 12); // in the cell from 8 to 16
    grid.add(near);
    expect(grid.around(at(16.5, 11, 2), at(22, 11))).toEqual([near]); // the camera 0.6 m from it, east in the next cell
    expect(grid.around(at(18.5, 11, 2), at(22, 11))).toEqual([]);
  });

  it('keeps giving a look till it’s whole again, wherever the camera went, then lets it go', () => {
    const grid = new LookGrid();
    const shown: number[] = [];
    const house = box(0, 0, 4, 4, shown);
    grid.add(house);
    const [camera, head] = [at(2, -8, 4), at(2, 8)]; // the line through it
    fade(camera, head, grid.around(camera, head), 50);
    expect(house.whole).toBe(false);
    const away = [at(100, 100, 10), at(100, 90)] as const;
    expect(grid.around(...away)).toEqual([house]);
    fade(...away, grid.around(...away), SETTLED);
    expect([house.whole, shown]).toEqual([true, [2, 0]]); // faded, then whole again
    expect(grid.around(...away)).toEqual([]);
  });

  it('forgets a look taken out, on its way back or not', () => {
    const grid = new LookGrid();
    const house = box(0, 0, 4, 4);
    const shed = box(1, 1, 2, 2);
    grid.add(house);
    grid.add(shed);
    const [camera, head] = [at(2, -8, 4), at(2, 8)]; // the line through it
    fade(camera, head, grid.around(camera, head), 50);
    grid.remove(house);
    expect(grid.around(camera, head)).toEqual([shed]);
    expect(grid.around(at(100, 100, 10), at(100, 90))).toEqual([shed]); // the shed on its way back; the house gone
  });

  it('boxes a model as placed: turned, scaled and moved', () => {
    const at = Matrix.Scaling(2, 2, 2)
      .multiply(Matrix.RotationY(Math.PI / 2))
      .multiply(Matrix.Translation(10, 1, 20));
    const { min, max } = placedBox({ min: new Vector3(-1, 0, -3), max: new Vector3(1, 4, 3) }, at);
    expect([...min.asArray(), ...max.asArray()].map((v) => Math.round(v * 1e9) / 1e9)).toEqual([
      4, 1, 18, 16, 9, 22,
    ]); // its 6 m along z turned to run along x, doubled
  });

  it('reads a line longer than the cells filed from those cells, and still only those under it', () => {
    const grid = new LookGrid();
    const [west, east] = [box(0, 0, 1, 1), box(100, 0, 101, 1)];
    grid.add(west);
    grid.add(east);
    expect(grid.around(at(-50, 0.5, 10), at(50, 0.5))).toEqual([west]); // 13 cells under it, 2 filed
    expect(grid.around(at(-50, 0.5, 10), at(150, 0.5))).toEqual([west, east]);
  });
});

describe('fade', () => {
  /** The meshes drawn faded now. */
  const faded = () => drawn().filter((m) => m.material?.name.endsWith(' 0.3'));

  it('fades a solid between the camera and the head to 30% without its ink, and brings it back', async () => {
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const house = solidLook(place, map.solids[0]!);
    const mid = house.min.add(house.max).scale(0.5);
    const up = new Vector3(0, 10, -10);
    fadeIn(place, mid.add(up), mid.subtract(up), SETTLED);
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

    fadeIn(place, new Vector3(-50, 10, -50), new Vector3(-40, 1, -40), SETTLED);
    expect(faded()).toEqual([]);
    expect((await built(body.material)).needAlphaBlending()).toBe(false);
    expect(
      body
        .getChildMeshes(true)
        .find((c) => c.name.endsWith(' ink'))
        ?.isEnabled(),
    ).toBe(true);
  });

  it('takes a solid from the view, ink and all, when the camera is in it or within 2 m; its shadow still cast', async () => {
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const house = solidLook(place, map.solids[0]!);
    const model = map.solids[0]!.model;
    const mid = house.min.add(house.max).scale(0.5);
    const gone = () => drawn().filter((m) => m.material?.name.endsWith(' 0'));
    const east = (d: number) => new Vector3(house.max.x + d, mid.y, mid.z); // beside it, the head further east
    const ink = (m: AbstractMesh) => m.getChildMeshes(true).find((c) => c.name.endsWith(' ink'));

    fadeIn(place, mid, mid.add(new Vector3(0, -10, 0)), SETTLED);
    expect(gone().map((m) => m.name)).toEqual([model, `${model} roof`]);
    for (const m of gone()) {
      expect(ink(m)?.isEnabled()).toBe(false);
    }
    await castShadows(gone());
    fadeIn(place, east(1.9), east(5), SETTLED);
    expect(gone().map((m) => m.name)).toEqual([model, `${model} roof`]);

    fadeIn(place, east(2.1), east(5), SETTLED);
    expect(gone()).toEqual([]);
    expect(faded()).toEqual([]);
    expect(ink(scene.getMeshByName(model)!)?.isEnabled()).toBe(true);
  });

  it('steps a solid toward how the camera wants it, one alpha each 50 ms: 1, 0.65, 0.3, 0.15, 0, and back the same way', async () => {
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const house = solidLook(place, map.solids[0]!);
    const body = scene.getMeshByName(map.solids[0]!.model)!;
    const mid = house.min.add(house.max).scale(0.5);
    const below = mid.add(new Vector3(0, -10, 0));
    const away = [new Vector3(-50, 10, -50), new Vector3(-40, 1, -40)] as const;
    const shown = () => {
      const alpha = / (0(?:\.\d+)?)$/.exec(body.material!.name)?.[1] ?? '1';
      const ink = body.getChildMeshes(true).find((c) => c.name.endsWith(' ink'))!;
      return ink.isEnabled() ? `${alpha} inked` : alpha;
    };
    const steps = (camera: Vector3, head: Vector3, dts: readonly number[]) =>
      dts.map((dt) => {
        fade(camera, head, [house], dt);
        return shown();
      });

    expect(steps(mid, below, [0, 49, 1, 50, 50, 50])).toEqual([
      '0.65',
      '0.65',
      '0.3',
      '0.15',
      '0',
      '0',
    ]); // in it: gone
    expect(steps(...away, [0, 50, 50, 50])).toEqual(['0.15', '0.3', '0.65', '1 inked']);
    expect(
      steps(mid.add(new Vector3(0, 10, -10)), mid.subtract(new Vector3(0, 10, -10)), [0, 50, 50]),
    ).toEqual(['0.65', '0.3', '0.3']); // across the camera's line: faded
    expect(steps(...away, [200])).toEqual(['1 inked']); // a long frame takes every step it covers
  });

  it('leaves a solid beside the camera’s line alone, and one past the head', async () => {
    const map = await mapOf('yard');
    const place = await buildPlace(scene, map, lighting, new Models(scene, source));
    const s = solidLook(place, map.solids[0]!);
    const mid = s.min.add(s.max).scale(0.5);
    const beside = new Vector3(s.max.x - s.min.x + 1, 0, 0);
    fade(mid.add(beside).add(new Vector3(0, 10, -10)), mid.add(beside), [s], SETTLED);
    expect(faded()).toEqual([]);
    const north = new Vector3(0, 0, s.max.z - mid.z + 1);
    fade(mid.add(north).add(new Vector3(0, 10, 10)), mid.add(north), [s], SETTLED); // the head stands past it, the camera beyond
    expect(faded()).toEqual([]);
  });
});
