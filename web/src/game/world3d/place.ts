// A map drawn (3d-world.md section 10): its sky and fog, its ground from the height grid, its solids and props as kit models or stand-ins, and its portals' glow.
import type { Material } from '@babylonjs/core/Materials/material';
import type { PBRMaterial } from '@babylonjs/core/Materials/PBR/pbrMaterial';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3, Color4 } from '@babylonjs/core/Maths/math.color';
import { Matrix, Quaternion, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import { CreateCylinder } from '@babylonjs/core/Meshes/Builders/cylinderBuilder';
import { CreateDisc } from '@babylonjs/core/Meshes/Builders/discBuilder';
import { CreateGround } from '@babylonjs/core/Meshes/Builders/groundBuilder';
import { CreateSphere } from '@babylonjs/core/Meshes/Builders/sphereBuilder';
import { CreateTorus } from '@babylonjs/core/Meshes/Builders/torusBuilder';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import { SubMesh } from '@babylonjs/core/Meshes/subMesh';
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';

import type { Terrain } from '@/sim/terrain';
import { height } from '@/sim/terrain';

import type { Lighting } from './body';
import { kitModels } from './kit-models';
import type { KitModel } from './kit-models';
import { groundUnder } from './map';
import type { Prop, Solid, WorldMap } from './map';
import type { Models } from './models';
import { linearOf } from './toon';
import type { Palette } from './toon';

/** A sky's colour, sRGB, and its fog from start to end, metres from the camera: the player stands about 16 m off. */
interface Sky {
  readonly color: number;
  readonly fog: readonly [number, number];
}

const SKIES: Readonly<Record<string, Sky>> = {
  day: { color: 0x9ecbf5, fog: [45, 140] },
  dawn: { color: 0xf3c9a5, fog: [40, 120] },
  dusk: { color: 0x7d6292, fog: [35, 110] },
  desert: { color: 0xf1dcae, fog: [40, 125] },
  snow: { color: 0xd9e5ef, fog: [30, 95] },
  shroom: { color: 0x8c7fb8, fog: [26, 80] },
  cave: { color: 0x18161f, fog: [17, 42] },
};

const GROUNDS: Readonly<Record<string, number>> = {
  grass: 0x8cc265,
  sand: 0xe2cf9a,
  snow: 0xeef2f6,
  dirt: 0xa88b62,
  stone: 0x6f6a63,
  brick: 0x9c5d4b,
};
const GROUND_DEFAULT = 0x9a9a9a;
const CLIFF = 0.6; // ground too steep to walk keeps this share of its colour, so a rise reads whichever way it faces the sun

/** A solid's stand-in: its collision shape in color, a tree's canopy or a house's roof above it in top. */
interface StandIn {
  readonly kind: 'plain' | 'tree' | 'pine' | 'house';
  readonly color: number;
  readonly top?: number;
}

const STAND_INS: Readonly<Record<string, StandIn>> = {
  tree_oak: { kind: 'tree', color: 0x7a5232, top: 0x4f9140 },
  tree_pine: { kind: 'pine', color: 0x6e4b2c, top: 0x2f6e44 },
  stump: { kind: 'plain', color: 0x8a6a42 },
  well: { kind: 'plain', color: 0x9b9b98 },
  rock_large: { kind: 'plain', color: 0x8b8a86 },
  boulder: { kind: 'plain', color: 0x9a958c },
  crate: { kind: 'plain', color: 0xa0703c },
  house_small: { kind: 'house', color: 0xe0d2b4, top: 0xa34a3a },
  house_large: { kind: 'house', color: 0xd8c8a6, top: 0x8f3f34 },
  shop: { kind: 'house', color: 0xe6d9b8, top: 0x3d6fa3 },
};
const STAND_IN_DEFAULT: StandIn = { kind: 'plain', color: 0xb4b0a8 };

/** A prop's stand-in: a tuft of grass or a bloom, size metres tall. */
interface PropStandIn {
  readonly shape: 'tuft' | 'bloom';
  readonly color: number;
  readonly size: number;
}

const PROP_STAND_INS: Readonly<Record<string, PropStandIn>> = {
  grass_tuft: { shape: 'tuft', color: 0x6fa84a, size: 0.35 },
  flowers: { shape: 'bloom', color: 0xe98aa8, size: 0.22 },
  fountain_flowers: { shape: 'bloom', color: 0xf0c04a, size: 0.3 },
};
const PROP_DEFAULT: PropStandIn = { shape: 'bloom', color: 0xc8c8c0, size: 0.25 };

const PORTAL = 0x7ac7ff;
const FADED = 0.3; // a solid between the camera and the player's head
const GONE = 0; // a solid at the lens, its shadow kept: faded it would still fill the view
const NEAR = 2; // metres from the camera within which a solid's drawn box is at the lens
const SINK = 0.5; // a solid reaches this far under the ground at its centre, so a slope never shows its foot
const APRON = 40; // flat ground past the map's edges, which the camera sees from near one
const DEG = Math.PI / 180;

/** One mesh of a stand-in solid, with its material as drawn, as faded and as gone. */
interface Shade {
  readonly mesh: AbstractMesh;
  readonly opaque: Material;
  readonly faded: Material;
  readonly gone: Material;
}

/** How a solid shows: as it is, faded out of the camera's way, or gone from the view at the lens but for its shadow. */
export type Seen = 'whole' | 'faded' | 'gone';

/** A solid as drawn: the box round it in the world, which fades out of the camera's way, and how it shows as seen. */
export class SolidLook {
  private seen: Seen = 'whole';

  constructor(
    readonly min: Vector3,
    readonly max: Vector3,
    private readonly show: (seen: Seen) => void,
  ) {}

  /** Draws it as seen: whole, or faded or gone without its ink. */
  showAs(seen: Seen): void {
    if (seen === this.seen) {
      return;
    }
    this.seen = seen;
    this.show(seen);
  }
}

export interface Place {
  readonly solids: readonly SolidLook[];
  dispose(): void;
}

/** A kit model's placements on a map: its solids by their index among the map's, then its props. */
interface Placed {
  readonly kit: KitModel;
  readonly solids: { readonly i: number; readonly at: Matrix }[];
  readonly props: Matrix[];
}

/** Draws map in scene: its sky, ground, solids, props and portals; a model key with a kit model draws it, any other a stand-in. */
export async function buildPlace(
  scene: Scene,
  map: WorldMap,
  lighting: Lighting,
  models: Models,
): Promise<Place> {
  const root = new TransformNode(`place ${map.id}`, scene);
  const sky = SKIES[map.sky] ?? SKIES.day!;
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(hex(sky.color)), 1);
  scene.fogMode = Scene.FOGMODE_LINEAR;
  scene.fogColor = linearOf(sky.color); // the toon materials fog in linear light, then go to gamma
  [scene.fogStart, scene.fogEnd] = sky.fog;

  const { palette } = lighting;
  const g = map.geometry;
  const groundColor = linearOf(GROUNDS[map.ground] ?? GROUND_DEFAULT);
  const { ground, cliffs } = groundMeshes(scene, g.terrain);
  ground.material = palette.toon({ texture: null, color: groundColor });
  if (cliffs) {
    cliffs.material = palette.toon({ texture: null, color: groundColor.scale(CLIFF) });
  }
  for (const mesh of [ground, cliffs]) {
    if (mesh) {
      mesh.parent = root;
      mesh.receiveShadows = true;
    }
  }
  const apron = CreateGround(
    'apron',
    { width: g.size_x + 2 * APRON, height: g.size_z + 2 * APRON },
    scene,
  );
  apron.parent = root;
  apron.position.set(g.size_x / 2, lowest(g.terrain) - 0.05, g.size_z / 2);
  apron.material = palette.toon({ texture: null, color: groundColor.scale(0.8) });
  apron.receiveShadows = true;

  const solids: SolidLook[] = [];
  const fades: [Material, AbstractMesh][] = [];
  const placed = new Map<string, Placed>();
  const kitFor = (key: string): Placed | undefined => {
    const kit = kitModels[key];
    if (!kit) {
      return undefined;
    }
    let p = placed.get(key);
    if (!p) {
      p = { kit, solids: [], props: [] };
      placed.set(key, p);
    }
    return p;
  };
  map.solids.forEach((s, i) => {
    const at = s.kind === 'box' ? s.box : s.cylinder;
    const p = kitFor(s.model);
    if (p) {
      const yaw = s.kind === 'box' ? s.yaw : 0;
      p.solids.push({ i, at: placement(p.kit, at.x, at.z, yaw, height(g.terrain, at.x, at.z)) });
      return;
    }
    solids[i] = standInSolid(scene, s, g.terrain, lighting, root, fades);
  });
  const scattered = new Map<string, Prop[]>();
  for (const prop of map.props) {
    const p = kitFor(prop.model);
    if (p) {
      p.props.push(placement(p.kit, prop.x, prop.z, prop.yaw, height(g.terrain, prop.x, prop.z)));
    } else {
      scattered.set(prop.model, [...(scattered.get(prop.model) ?? []), prop]);
    }
  }
  await Promise.all(
    [...placed].map(async ([key, p]) => {
      const parts = await kitParts(models, p.kit, key, lighting, root);
      p.solids.forEach(({ i, at }, n) => {
        solids[i] = kitSolid(parts, at, n, lighting, root, fades); // its faded copy made before any instance, which a clone would carry
      });
      const all = [...p.solids.map((s) => s.at), ...p.props];
      for (const part of parts) {
        part.mesh.thinInstanceAdd(all);
        part.ink?.thinInstanceAdd(all);
      }
    }),
  );
  scatter(scene, scattered, g.terrain, palette, root);
  for (const p of map.portals) {
    const y = groundUnder(map, p.x, p.z);
    const disc = CreateDisc(`portal ${p.name}`, { radius: p.radius, tessellation: 48 }, scene);
    disc.parent = root;
    disc.rotation.x = Math.PI / 2;
    disc.position.set(p.x, y + 0.03, p.z);
    disc.material = palette.glow(linearOf(PORTAL), 0.35);
    const ring = CreateTorus(
      `portal ${p.name} ring`,
      { diameter: 2 * p.radius, thickness: 0.08, tessellation: 48 },
      scene,
    );
    ring.parent = root;
    ring.position.set(p.x, y + 0.05, p.z);
    ring.material = palette.glow(linearOf(PORTAL), 0.9);
  }
  warm(fades);

  return {
    solids,
    dispose: () => {
      root.dispose(false, false);
      scene.fogMode = Scene.FOGMODE_NONE;
    },
  };
}

/** Takes each solid within NEAR of the camera from the view, fades each other between camera and head to 30%, and shows the rest whole. */
export function fade(camera: Vector3, head: Vector3, solids: readonly SolidLook[]): void {
  for (const s of solids) {
    s.showAs(
      near(camera, s.min, s.max) ? 'gone' : crosses(camera, head, s.min, s.max) ? 'faded' : 'whole',
    );
  }
}

/** Whether p is within NEAR of the box from min to max, or in it. */
function near(p: Vector3, min: Vector3, max: Vector3): boolean {
  let d2 = 0;
  for (const axis of AXES) {
    const out = Math.max(min[axis] - p[axis], 0, p[axis] - max[axis]);
    d2 += out * out;
  }
  return d2 <= NEAR * NEAR;
}

/** Builds each faded material's shaders for a mesh it draws, so a solid's first fade doesn't stall a frame; as forceCompilation does, but stopping once the place is gone. */
function warm(fades: readonly (readonly [Material, AbstractMesh])[]): void {
  const done = new Set<Material>();
  for (const [m, mesh] of fades) {
    if (done.has(m)) {
      continue;
    }
    done.add(m);
    const trial = new SubMesh(0, 0, 0, 0, 0, mesh, undefined, false, false); // its own, so the mesh's drawn effect stays
    const check = () => {
      if (!mesh.isDisposed() && !m.isReadyForSubMesh(mesh, trial)) {
        setTimeout(check, WARM_MS);
      }
    };
    check();
  }
}

const WARM_MS = 16;

const AXES = ['x', 'y', 'z'] as const;

/** Whether the segment from a to b meets the box from min to max. */
function crosses(a: Vector3, b: Vector3, min: Vector3, max: Vector3): boolean {
  let from = 0;
  let to = 1;
  for (const axis of AXES) {
    const d = b[axis] - a[axis];
    if (d === 0) {
      if (a[axis] < min[axis] || a[axis] > max[axis]) {
        return false;
      }
      continue;
    }
    const u = (min[axis] - a[axis]) / d;
    const v = (max[axis] - a[axis]) / d;
    from = Math.max(from, Math.min(u, v));
    to = Math.min(to, Math.max(u, v));
    if (from > to) {
      return false;
    }
  }
  return true;
}

/** The ground: one vertex per height sample, cell metres apart, lit by its own normals; the cells the sim marks steep go to cliffs, null with none. */
function groundMeshes(scene: Scene, t: Terrain): { ground: Mesh; cliffs: Mesh | null } {
  const positions: number[] = [];
  const flat: number[] = [];
  const steep: number[] = [];
  for (let iz = 0; iz < t.nz; iz++) {
    for (let ix = 0; ix < t.nx; ix++) {
      positions.push(ix * t.cell, t.heights[iz * t.nx + ix]!, iz * t.cell);
    }
  }
  for (let iz = 0; iz < t.nz - 1; iz++) {
    for (let ix = 0; ix < t.nx - 1; ix++) {
      const a = iz * t.nx + ix;
      const c = a + t.nx;
      const k = iz * (t.nx - 1) + ix;
      (t.steepX[k] || t.steepZ[k] ? steep : flat).push(a, a + 1, c, a + 1, c + 1, c); // clockwise from above, Babylon's front
    }
  }
  const normals: number[] = [];
  VertexData.ComputeNormals(positions, [...flat, ...steep], normals); // one set, so the light runs on across the seam
  const mesh = (name: string, indices: number[]): Mesh => {
    const data = new VertexData();
    data.positions = positions;
    data.indices = indices;
    data.normals = normals;
    const m = new Mesh(name, scene);
    data.applyToMesh(m);
    return m;
  };
  return { ground: mesh('ground', flat), cliffs: steep.length > 0 ? mesh('cliffs', steep) : null };
}

/** The lowest of the terrain's heights. */
function lowest(t: Terrain): number {
  let low = Infinity;
  for (const h of t.heights) {
    low = Math.min(low, h);
  }
  return low;
}

/** The world box round meshes. */
function boxOf(meshes: readonly AbstractMesh[]): { min: Vector3; max: Vector3 } {
  const min = new Vector3(Infinity, Infinity, Infinity);
  const max = new Vector3(-Infinity, -Infinity, -Infinity);
  for (const mesh of meshes) {
    mesh.computeWorldMatrix(true);
    const b = mesh.getBoundingInfo().boundingBox;
    min.minimizeInPlace(b.minimumWorld);
    max.maximizeInPlace(b.maximumWorld);
  }
  return { min, max };
}

/** A stand-in for a solid with no kit model: its collision shape in the model key's colour, with a tree's canopy or a house's roof; it fades by its materials. */
function standInSolid(
  scene: Scene,
  s: Solid,
  t: Terrain,
  lighting: Lighting,
  root: TransformNode,
  fades: [Material, AbstractMesh][],
): SolidLook {
  const { palette, shadows } = lighting;
  const look = STAND_INS[s.model] ?? STAND_IN_DEFAULT;
  const at = s.kind === 'box' ? s.box : s.cylinder;
  const bottom = height(t, at.x, at.z) - SINK;
  const tall = at.top - bottom;
  const node = new TransformNode(`solid ${s.model}`, scene);
  node.parent = root;
  node.position.set(at.x, bottom, at.z);
  const shades: Shade[] = [];
  const inks: AbstractMesh[] = [];
  const add = (mesh: Mesh, color: number) => {
    mesh.parent ??= node;
    const base = { texture: null, color: linearOf(color) };
    const opaque = palette.toon(base);
    const faded = palette.toon(base, FADED);
    const gone = palette.toon(base, GONE);
    mesh.material = opaque;
    mesh.receiveShadows = true;
    shadows.addShadowCaster(mesh, false);
    shades.push({ mesh, opaque, faded, gone });
    fades.push([faded, mesh], [gone, mesh]);
    palette.outline(mesh);
    inks.push(...mesh.getChildMeshes(true).filter((c) => c.name === `${mesh.name} ink`));
  };
  if (s.kind === 'box') {
    const { half_w: hw, half_d: hd, ux, uz } = s.box;
    node.rotation.y = Math.atan2(-uz, ux); // the box's width runs along (ux, uz)
    const walls = look.kind === 'house' ? tall * 0.6 : tall;
    const body = CreateBox(s.model, { width: 2 * hw, depth: 2 * hd, height: walls }, scene);
    body.position.y = walls / 2;
    add(body, look.color);
    if (look.kind === 'house') {
      const span = new TransformNode(`${s.model} roof span`, scene);
      span.parent = node;
      span.position.y = walls;
      span.scaling.set((2 * hw * 1.15) / Math.SQRT1_2, 1, (2 * hd * 1.15) / Math.SQRT1_2); // a square pyramid's side is √½ of its diameter
      const roof = CreateCylinder(
        `${s.model} roof`,
        { diameterTop: 0, diameterBottom: 1, height: tall - walls, tessellation: 4 },
        scene,
      );
      roof.parent = span;
      roof.rotation.y = Math.PI / 4; // its corners onto the walls' corners
      roof.position.y = (tall - walls) / 2;
      add(roof, look.top ?? look.color);
    }
  } else {
    const r = s.cylinder.radius;
    if (look.kind === 'tree' || look.kind === 'pine') {
      const crown = Math.min(tall * (look.kind === 'tree' ? 0.35 : 0.3), 4 * r);
      const trunkTop = look.kind === 'tree' ? tall - crown : tall * 0.3;
      const trunk = CreateCylinder(s.model, { diameter: 2 * r, height: trunkTop + 0.2 }, scene);
      trunk.position.y = (trunkTop + 0.2) / 2;
      add(trunk, look.color);
      const canopy =
        look.kind === 'tree'
          ? CreateSphere(`${s.model} canopy`, { diameter: 2 * crown, segments: 8 }, scene)
          : CreateCylinder(
              `${s.model} canopy`,
              {
                diameterTop: 0,
                diameterBottom: 2 * crown,
                height: tall - trunkTop,
                tessellation: 10,
              },
              scene,
            );
      canopy.position.y = look.kind === 'tree' ? tall - crown : (tall + trunkTop) / 2;
      add(canopy, look.top ?? look.color);
    } else {
      const body = CreateCylinder(
        s.model,
        { diameter: 2 * r, height: tall, tessellation: 16 },
        scene,
      );
      body.position.y = tall / 2;
      add(body, look.color);
    }
  }
  const { min, max } = boxOf(shades.map((sh) => sh.mesh));
  return new SolidLook(min, max, (seen) => {
    for (const sh of shades) {
      sh.mesh.material = seen === 'whole' ? sh.opaque : seen === 'faded' ? sh.faded : sh.gone;
    }
    for (const ink of inks) {
      ink.setEnabled(seen === 'whole');
    }
  });
}

/** Where a kit model stands: scaled, turned by its yaw, raised over the ground at (x, z). */
function placement(kit: KitModel, x: number, z: number, yawDeg: number, ground: number): Matrix {
  return Matrix.Compose(
    new Vector3(kit.scale, kit.scale, kit.scale),
    Quaternion.RotationAxis(Vector3.Up(), yawDeg * DEG),
    new Vector3(x, ground + kit.y, z),
  );
}

/** A kit model's mesh in the model's own space, drawn once per placement as a thin instance with its ink, and its faded and gone materials. */
interface KitPart {
  readonly mesh: Mesh;
  readonly ink: Mesh | null;
  readonly faded: Material;
  readonly gone: Material;
}

/** key's kit model, made once: each mesh baked into the model's space at an identity transform, toon-drawn, inked and casting shadows; its clips and skeletons let go, as it stands still. */
async function kitParts(
  models: Models,
  kit: KitModel,
  key: string,
  lighting: Lighting,
  root: TransformNode,
): Promise<KitPart[]> {
  const { palette, shadows } = lighting;
  const container = await models.load(kit.file);
  const made = container.instantiateModelsToScene((n) => `${key} ${n}`, false, {
    doNotInstantiate: true,
  });
  for (const a of made.animationGroups) {
    a.dispose();
  }
  const meshes = made.rootNodes
    .flatMap((r) => r.getChildMeshes(false))
    .filter((m): m is Mesh => m instanceof Mesh && m.material !== null && m.getTotalVertices() > 0);
  const worlds = meshes.map((m) => m.computeWorldMatrix(true).clone());
  const parts = meshes.map((mesh, i): KitPart => {
    const base = baseOf(mesh.material!);
    mesh.makeGeometryUnique();
    if (mesh.skeleton) {
      mesh.skeleton.prepare(true);
      mesh.applySkeleton(mesh.skeleton); // its rest pose, which only its bones bring to size
      mesh.skeleton = null;
    }
    mesh.parent = root;
    mesh.bakeTransformIntoVertices(worlds[i]!); // flips its faces where the glTF root mirrors it
    mesh.position.setAll(0);
    mesh.rotationQuaternion = null;
    mesh.rotation.setAll(0);
    mesh.scaling.setAll(1);
    mesh.refreshBoundingInfo();
    mesh.material = palette.toon(base);
    mesh.receiveShadows = true;
    shadows.addShadowCaster(mesh, false);
    palette.outline(mesh);
    const ink = mesh
      .getChildMeshes(true)
      .find((c): c is Mesh => c instanceof Mesh && c.name === `${mesh.name} ink`);
    return {
      mesh,
      ink: ink ?? null,
      faded: palette.toon(base, FADED),
      gone: palette.toon(base, GONE),
    };
  });
  for (const r of made.rootNodes) {
    r.dispose(false, false); // what's left of its tree once its meshes moved out
  }
  for (const k of made.skeletons) {
    k.dispose();
  }
  return parts;
}

const HIDDEN = Matrix.Scaling(0, 0, 0);

/** A kit solid: thin instance n of parts at at, and a copy of it alone, faded or gone, hidden till the camera's way meets it. */
function kitSolid(
  parts: readonly KitPart[],
  at: Matrix,
  n: number,
  lighting: Lighting,
  root: TransformNode,
  fades: [Material, AbstractMesh][],
): SolidLook {
  const copies = parts.map((p) => {
    const c = p.mesh.clone(`${p.mesh.name} faded`, root, true);
    c.material = p.faded;
    c.rotationQuaternion = new Quaternion();
    at.decompose(c.scaling, c.rotationQuaternion, c.position);
    c.receiveShadows = true;
    lighting.shadows.addShadowCaster(c, false);
    c.setEnabled(false);
    fades.push([p.faded, c], [p.gone, c]);
    return c;
  });
  const { min, max } = boxOf(copies);
  return new SolidLook(min, max, (seen) => {
    parts.forEach((p, k) => {
      const whole = seen === 'whole';
      p.mesh.thinInstanceSetMatrixAt(n, whole ? at : HIDDEN);
      p.ink?.thinInstanceSetMatrixAt(n, whole ? at : HIDDEN);
      copies[k]!.material = seen === 'gone' ? p.gone : p.faded;
      copies[k]!.setEnabled(!whole);
    });
  });
}

/** The toon base for a glTF material: its texture and colour as authored. */
function baseOf(source: Material): { texture: Texture | null; color: Color3 } {
  const pbr = source as Partial<Pick<PBRMaterial, 'albedoTexture' | 'albedoColor'>>;
  return {
    texture: pbr.albedoTexture instanceof Texture ? pbr.albedoTexture : null,
    color: pbr.albedoColor ?? Color3.White(),
  };
}

/** The props with no kit model: one stand-in per model key, drawn at each point on the ground as thin instances. */
function scatter(
  scene: Scene,
  byModel: ReadonlyMap<string, readonly Prop[]>,
  t: Terrain,
  palette: Palette,
  root: TransformNode,
): void {
  for (const [key, points] of byModel) {
    const look = PROP_STAND_INS[key] ?? PROP_DEFAULT;
    const mesh =
      look.shape === 'tuft'
        ? CreateCylinder(
            `prop ${key}`,
            { diameterTop: 0, diameterBottom: look.size * 0.8, height: look.size, tessellation: 5 },
            scene,
          )
        : CreateSphere(`prop ${key}`, { diameter: look.size, segments: 4 }, scene);
    mesh.parent = root;
    mesh.material = palette.toon({ texture: null, color: linearOf(look.color) });
    mesh.receiveShadows = true;
    mesh.thinInstanceAdd(
      points.map((p) =>
        Matrix.Compose(
          Vector3.One(),
          Quaternion.RotationAxis(Vector3.Up(), p.yaw * DEG),
          new Vector3(p.x, height(t, p.x, p.z) + look.size / 2, p.z),
        ),
      ),
    );
  }
}

function hex(rgb: number): string {
  return `#${rgb.toString(16).padStart(6, '0')}`;
}
