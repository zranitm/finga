// A map drawn (3d-world.md section 10): its sky and fog, its ground from the height grid, its solids and props as kit models or stand-ins, and its portals' glow.
import type { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator';
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
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';

import { movement } from '@/gamedata/movement';
import type { Terrain } from '@/sim/terrain';
import { height } from '@/sim/terrain';

import type { Lighting } from './body';
import { CLIFF, groundOf } from './grounds';
import { kitModels } from './kit-models';
import type { KitModel } from './kit-models';
import { groundUnder } from './map';
import type { Prop, Solid, WorldMap } from './map';
import type { Models } from './models';
import { linearOf, warm } from './toon';
import type { Palette } from './toon';

/** A sky's colour, sRGB, and its fog from start to end, metres from the camera: the player stands about 16 m off, close up. */
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
const FOG_OUT = 3.6; // zoomed right out, the fog starts and ends this many times as far: a day sky's from about 160 m to 500 m (large-regions.md section 9)

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
const NEAR = 2; // metres from the camera within which a look's drawn box is at the lens
const STEPS = [1, 0.65, 0.3, 0.15, 0] as const; // the alphas a look steps through: whole, faded at 0.3, gone at 0
const STEP_MS = 50; // between two steps, so whole to gone takes 0.15 s after the first
const LOW_PROP = movement.height / 2; // a prop no taller hides at most a player's legs, so it never fades
const SINK = 0.5; // a solid reaches this far under the ground at its centre, so a slope never shows its foot
const APRON = 500; // flat ground past the map's edges, out to where the fog hides it with the camera zoomed right out
const DEG = Math.PI / 180;

/** One mesh of a stand-in solid, with its material at each of STEPS. */
interface Shade {
  readonly mesh: AbstractMesh;
  readonly steps: readonly Material[];
}

/** How a look shows: as it is, faded out of the camera's way, or gone from the view at the lens but for its shadow. */
export type Seen = 'whole' | 'faded' | 'gone';

const AT: Readonly<Record<Seen, number>> = { whole: 0, faded: 2, gone: 4 }; // each one's step in STEPS

/** A solid or prop as drawn: the box round it in the world, and how it shows at each of STEPS on its way to how the camera wants it. */
export class Look {
  private step = 0;
  private since = STEP_MS; // ms since its last step, a full one at rest so a change starts at once

  constructor(
    readonly min: Vector3,
    readonly max: Vector3,
    private readonly show: (step: number) => void,
  ) {}

  /** Steps it toward seen over dtMs, one of STEPS each STEP_MS: past the first it's see-through, without its ink. */
  toward(seen: Seen, dtMs: number): void {
    const to = AT[seen];
    if (to === this.step) {
      this.since = STEP_MS;
      return;
    }
    this.since += dtMs;
    let step = this.step;
    while (step !== to && this.since >= STEP_MS) {
      step += Math.sign(to - step);
      this.since -= STEP_MS;
    }
    if (step === to) {
      this.since = STEP_MS; // at rest again, so the next change starts at once
    }
    if (step !== this.step) {
      this.step = step;
      this.show(step);
    }
  }
}

export interface Place {
  /** Each solid's look by its index among the map's, then the kit props' taller than LOW_PROP in the map's order. */
  readonly looks: readonly Look[];
  /** Draws what the camera's target at (x, z) needs: a region streams its chunks round it, a map is drawn whole. */
  follow(x: number, z: number): void;
  dispose(): void;
}

/** A kit model's placements on a map: its solids and props, each by its index among the map's. */
interface Placed {
  readonly kit: KitModel;
  readonly solids: { readonly i: number; readonly at: Matrix }[];
  readonly props: { readonly i: number; readonly at: Matrix }[];
}

/** Sets scene's fog for a sky seen at zoom, 0 close to 1 far: it draws back as the camera does. */
export function fogFor(scene: Scene, sky: string, zoom: number): void {
  const [start, end] = (SKIES[sky] ?? SKIES.day!).fog;
  const far = 1 + (FOG_OUT - 1) * zoom;
  scene.fogStart = start * far;
  scene.fogEnd = end * far;
}

/** Sets scene's sky: its colour behind everything and its fog, close up. */
export function skyFor(scene: Scene, key: string): void {
  const sky = SKIES[key] ?? SKIES.day!;
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(hex(sky.color)), 1);
  scene.fogMode = Scene.FOGMODE_LINEAR;
  scene.fogColor = linearOf(sky.color); // the toon materials fog in linear light, then go to gamma
  fogFor(scene, key, 0);
}

/** Draws map in scene: its sky, ground, solids, props and portals; a model key with a kit model draws it, any other a stand-in. */
export async function buildPlace(
  scene: Scene,
  map: WorldMap,
  lighting: Lighting,
  models: Models,
): Promise<Place> {
  const root = new TransformNode(`place ${map.id}`, scene);
  skyFor(scene, map.sky);

  const { palette } = lighting;
  const g = map.geometry;
  const groundColor = linearOf(groundOf(map.ground));
  const { ground, cliffs } = groundMeshes(scene, g.terrain);
  ground.material = palette.toon({ texture: null, color: groundColor }, 1, true);
  if (cliffs) {
    cliffs.material = palette.toon({ texture: null, color: groundColor.scale(CLIFF) }, 1, true);
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
  apron.material = palette.apron({ texture: null, color: groundColor.scale(0.8) });
  apron.receiveShadows = true;

  const solids: Look[] = [];
  const props: (Look | undefined)[] = []; // by kit prop, none for a low one
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
  let kitProps = 0;
  for (const prop of map.props) {
    const p = kitFor(prop.model);
    if (p) {
      const at = placement(p.kit, prop.x, prop.z, prop.yaw, height(g.terrain, prop.x, prop.z));
      p.props.push({ i: kitProps++, at });
    } else {
      scattered.set(prop.model, [...(scattered.get(prop.model) ?? []), prop]);
    }
  }
  await Promise.all(
    [...placed].map(async ([key, p]) => {
      const parts = await kitParts(models, p.kit, key, lighting.palette, lighting.shadows, root);
      const all = [...p.solids, ...p.props].map((s) => s.at);
      p.solids.forEach(({ i, at }, n) => {
        solids[i] = kitLook(parts, at, n, lighting, root, fades); // its copy made before any instance, which a clone would carry
      });
      const { min, max } = boxOf(parts.map((part) => part.mesh)); // in the model's space, before any instance
      if ((max.y - min.y) * p.kit.scale > LOW_PROP) {
        p.props.forEach(({ i, at }, k) => {
          props[i] = kitLook(parts, at, p.solids.length + k, lighting, root, fades);
        });
      }
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
    looks: [...solids, ...props.filter((l): l is Look => l !== undefined)],
    follow: () => {
      // a map is drawn whole: nothing streams
    },
    dispose: () => {
      root.dispose(false, false);
      scene.fogMode = Scene.FOGMODE_NONE;
    },
  };
}

/** Steps each look over dtMs toward gone within NEAR of the camera, faded between camera and head, and whole elsewhere. */
export function fade(camera: Vector3, head: Vector3, looks: readonly Look[], dtMs: number): void {
  for (const s of looks) {
    s.toward(
      near(camera, s.min, s.max) ? 'gone' : crosses(camera, head, s.min, s.max) ? 'faded' : 'whole',
      dtMs,
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
): Look {
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
    const steps = STEPS.map((a) => palette.toon(base, a, true));
    mesh.material = steps[0]!;
    mesh.receiveShadows = true;
    shadows.addShadowCaster(mesh, false);
    shades.push({ mesh, steps });
    fades.push(...steps.slice(1).map((m): [Material, AbstractMesh] => [m, mesh]));
    palette.outline(mesh, true);
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
  return new Look(min, max, (step) => {
    for (const sh of shades) {
      sh.mesh.material = sh.steps[step]!;
    }
    for (const ink of inks) {
      ink.setEnabled(step === 0);
    }
  });
}

/** Where a kit model stands: scaled, turned by its yaw, raised over the ground at (x, z). */
export function placement(
  kit: KitModel,
  x: number,
  z: number,
  yawDeg: number,
  ground: number,
): Matrix {
  return Matrix.Compose(
    new Vector3(kit.scale, kit.scale, kit.scale),
    Quaternion.RotationAxis(Vector3.Up(), yawDeg * DEG),
    new Vector3(x, ground + kit.y, z),
  );
}

/** A kit model's mesh in the model's own space, drawn once per placement as a thin instance with its ink, and its material at each of STEPS. */
export interface KitPart {
  readonly mesh: Mesh;
  readonly ink: Mesh | null;
  readonly steps: readonly Material[];
}

/** key's kit model, made once: each mesh baked into the model's space at an identity transform, toon-drawn, inked and casting shadows when given them; its clips and skeletons let go, as it stands still. */
export async function kitParts(
  models: Models,
  kit: KitModel,
  key: string,
  palette: Palette,
  shadows: ShadowGenerator | null,
  root: TransformNode,
): Promise<KitPart[]> {
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
    const steps = STEPS.map((a) => palette.toon(base, a, true));
    mesh.material = steps[0]!;
    mesh.receiveShadows = true;
    shadows?.addShadowCaster(mesh, false);
    palette.outline(mesh, true);
    const ink = mesh
      .getChildMeshes(true)
      .find((c): c is Mesh => c instanceof Mesh && c.name === `${mesh.name} ink`);
    return { mesh, ink: ink ?? null, steps };
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

/** A kit solid or prop: thin instance n of parts at at, and a see-through copy of it alone, hidden till the camera's way meets it. */
function kitLook(
  parts: readonly KitPart[],
  at: Matrix,
  n: number,
  lighting: Lighting,
  root: TransformNode,
  fades: [Material, AbstractMesh][],
): Look {
  const copies = parts.map((p) => {
    const c = p.mesh.clone(`${p.mesh.name} faded`, root, true);
    c.material = p.steps[1]!;
    c.rotationQuaternion = new Quaternion();
    at.decompose(c.scaling, c.rotationQuaternion, c.position);
    c.receiveShadows = true;
    lighting.shadows.addShadowCaster(c, false);
    c.setEnabled(false);
    fades.push(...p.steps.slice(1).map((m): [Material, AbstractMesh] => [m, c]));
    return c;
  });
  const { min, max } = boxOf(copies);
  let whole = true;
  return new Look(min, max, (step) => {
    parts.forEach((p, k) => {
      copies[k]!.material = p.steps[step]!;
    });
    if (whole === (step === 0)) {
      return;
    }
    whole = step === 0; // the instance swaps for its copy only here, as setting a thin instance uploads them all
    parts.forEach((p, k) => {
      p.mesh.thinInstanceSetMatrixAt(n, whole ? at : HIDDEN);
      p.ink?.thinInstanceSetMatrixAt(n, whole ? at : HIDDEN);
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
    mesh.material = palette.toon({ texture: null, color: linearOf(look.color) }, 1, true);
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
