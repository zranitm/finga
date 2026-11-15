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
  stone: 0x8e8d8b,
  brick: 0x9c5d4b,
};
const GROUND_DEFAULT = 0x9a9a9a;

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
const SINK = 0.5; // a solid reaches this far under the ground at its centre, so a slope never shows its foot
const APRON = 40; // flat ground past the map's edges, which the camera sees beyond a corridor
const DEG = Math.PI / 180;

/** One mesh of a solid, with its material as drawn and as faded. */
interface Shade {
  readonly mesh: AbstractMesh;
  readonly opaque: Material;
  readonly faded: Material;
}

/** A solid as drawn: its meshes and ink, and the box round them all, which fades out of the camera's way. */
export class SolidLook {
  private faded = false;

  constructor(
    readonly min: Vector3,
    readonly max: Vector3,
    private readonly shades: readonly Shade[],
    private readonly inks: readonly AbstractMesh[],
  ) {}

  /** Draws it faded, without its ink, or back as it was. */
  fadeTo(faded: boolean): void {
    if (faded === this.faded) {
      return;
    }
    this.faded = faded;
    for (const s of this.shades) {
      s.mesh.material = faded ? s.faded : s.opaque;
    }
    for (const ink of this.inks) {
      ink.setEnabled(!faded);
    }
  }
}

export interface Place {
  /** The map's size in metres, which holds the camera. */
  readonly sizeX: number;
  readonly sizeZ: number;
  readonly solids: readonly SolidLook[];
  dispose(): void;
}

/** Draws map in scene: its sky, ground, solids, props and portals; a model key in registry draws its kit model, any other a stand-in. */
export async function buildPlace(
  scene: Scene,
  map: WorldMap,
  lighting: Lighting,
  models: Models,
  registry: Readonly<Record<string, KitModel>> = kitModels,
): Promise<Place> {
  const root = new TransformNode(`place ${map.id}`, scene);
  const sky = SKIES[map.sky] ?? SKIES.day!;
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(hex(sky.color)), 1);
  scene.fogMode = Scene.FOGMODE_LINEAR;
  scene.fogColor = linearOf(sky.color); // the toon materials fog in linear light, then go to gamma
  [scene.fogStart, scene.fogEnd] = sky.fog;

  const { palette, shadows } = lighting;
  const g = map.geometry;
  const groundColor = linearOf(GROUNDS[map.ground] ?? GROUND_DEFAULT);
  const ground = groundMesh(scene, g.terrain);
  ground.parent = root;
  ground.material = palette.toon({ texture: null, color: groundColor });
  ground.receiveShadows = true;
  const apron = CreateGround(
    'apron',
    { width: g.size_x + 2 * APRON, height: g.size_z + 2 * APRON },
    scene,
  );
  apron.parent = root;
  apron.position.set(g.size_x / 2, Math.min(...g.terrain.heights) - 0.05, g.size_z / 2);
  apron.material = palette.toon({ texture: null, color: groundColor.scale(0.8) });
  apron.receiveShadows = true;

  const solids = await Promise.all(
    map.solids.map(async (s) => {
      const kit = registry[s.model];
      const look = kit
        ? await kitSolid(scene, models, kit, s, g.terrain, palette)
        : standInSolid(scene, s, g.terrain, palette);
      look.node.parent = root;
      for (const sh of look.shades) {
        shadows.addShadowCaster(sh.mesh, false);
        sh.mesh.receiveShadows = true;
      }
      return solidLook(look.shades, look.inks);
    }),
  );
  await drawProps(scene, models, map.props, g.terrain, palette, registry, root);
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

  return {
    sizeX: g.size_x,
    sizeZ: g.size_z,
    solids,
    dispose: () => {
      root.dispose(false, false);
      scene.fogMode = Scene.FOGMODE_NONE;
    },
  };
}

/** Fades each solid between camera and head to 30%, and the rest back to full. */
export function fade(camera: Vector3, head: Vector3, solids: readonly SolidLook[]): void {
  for (const s of solids) {
    s.fadeTo(crosses(camera, head, s.min, s.max));
  }
}

/** Whether the segment from a to b meets the box from min to max. */
function crosses(a: Vector3, b: Vector3, min: Vector3, max: Vector3): boolean {
  let from = 0;
  let to = 1;
  for (const axis of ['x', 'y', 'z'] as const) {
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

/** The ground: one vertex per height sample, cell metres apart, lit by its own normals. */
function groundMesh(scene: Scene, t: Terrain): Mesh {
  const positions: number[] = [];
  const indices: number[] = [];
  for (let iz = 0; iz < t.nz; iz++) {
    for (let ix = 0; ix < t.nx; ix++) {
      positions.push(ix * t.cell, t.heights[iz * t.nx + ix]!, iz * t.cell);
    }
  }
  for (let iz = 0; iz < t.nz - 1; iz++) {
    for (let ix = 0; ix < t.nx - 1; ix++) {
      const a = iz * t.nx + ix;
      const c = a + t.nx;
      indices.push(a, a + 1, c, a + 1, c + 1, c); // clockwise from above, Babylon's front
    }
  }
  const normals: number[] = [];
  VertexData.ComputeNormals(positions, indices, normals);
  const data = new VertexData();
  data.positions = positions;
  data.indices = indices;
  data.normals = normals;
  const mesh = new Mesh('ground', scene);
  data.applyToMesh(mesh);
  return mesh;
}

interface Drawn {
  readonly node: TransformNode;
  readonly shades: Shade[];
  readonly inks: AbstractMesh[];
}

/** A solid's look as the camera's way meets it: its meshes and the world box round them. */
function solidLook(shades: readonly Shade[], inks: readonly AbstractMesh[]): SolidLook {
  const min = new Vector3(Infinity, Infinity, Infinity);
  const max = new Vector3(-Infinity, -Infinity, -Infinity);
  for (const { mesh } of shades) {
    mesh.computeWorldMatrix(true);
    const b = mesh.getBoundingInfo().boundingBox;
    min.minimizeInPlace(b.minimumWorld);
    max.maximizeInPlace(b.maximumWorld);
  }
  return new SolidLook(min, max, shades, inks);
}

/** A stand-in for a solid with no kit model: its collision shape in the model key's colour, with a tree's canopy or a house's roof. */
function standInSolid(scene: Scene, s: Solid, t: Terrain, palette: Palette): Drawn {
  const look = STAND_INS[s.model] ?? STAND_IN_DEFAULT;
  const at = s.kind === 'box' ? s.box : s.cylinder;
  const bottom = height(t, at.x, at.z) - SINK;
  const tall = at.top - bottom;
  const node = new TransformNode(`solid ${s.model}`, scene);
  node.position.set(at.x, bottom, at.z);
  const drawn: Drawn = { node, shades: [], inks: [] };
  const add = (mesh: Mesh, color: number) => {
    mesh.parent ??= node;
    const base = { texture: null, color: linearOf(color) };
    const opaque = palette.toon(base);
    mesh.material = opaque;
    drawn.shades.push({ mesh, opaque, faded: palette.toon(base, FADED) });
    palette.outline(mesh);
    drawn.inks.push(...mesh.getChildMeshes(true).filter((c) => c.name === `${mesh.name} ink`));
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
    return drawn;
  }
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
    return drawn;
  }
  const body = CreateCylinder(s.model, { diameter: 2 * r, height: tall, tessellation: 16 }, scene);
  body.position.y = tall / 2;
  add(body, look.color);
  return drawn;
}

/** A solid drawn by its kit model: a copy of the file, turned by its yaw, scaled, raised and stood on the ground at its centre. */
async function kitSolid(
  scene: Scene,
  models: Models,
  kit: KitModel,
  s: Solid,
  t: Terrain,
  palette: Palette,
): Promise<Drawn> {
  const at = s.kind === 'box' ? s.box : s.cylinder;
  const yaw = s.kind === 'box' ? s.yaw : 0;
  return placeKit(scene, models, kit, s.model, at.x, at.z, yaw, height(t, at.x, at.z), palette);
}

async function placeKit(
  scene: Scene,
  models: Models,
  kit: KitModel,
  key: string,
  x: number,
  z: number,
  yawDeg: number,
  ground: number,
  palette: Palette,
): Promise<Drawn> {
  const container = await models.load(kit.file);
  const made = container.instantiateModelsToScene((n) => `${key} ${n}`, false, {
    doNotInstantiate: true,
  });
  for (const a of made.animationGroups) {
    a.dispose(); // a kit model stands still
  }
  const node = new TransformNode(`kit ${key}`, scene);
  node.position.set(x, ground + kit.y, z);
  node.rotation.y = yawDeg * DEG;
  node.scaling.setAll(kit.scale);
  const drawn: Drawn = { node, shades: [], inks: [] };
  for (const r of made.rootNodes) {
    r.parent = node;
  }
  for (const r of made.rootNodes) {
    for (const mesh of r.getChildMeshes(false)) {
      if (!mesh.material || mesh.getTotalVertices() === 0) {
        continue;
      }
      if (mesh.skeleton) {
        mesh.skeleton.prepare(true);
        mesh.refreshBoundingInfo({ applySkeleton: true });
      }
      const base = baseOf(mesh.material);
      const opaque = palette.toon(base);
      mesh.material = opaque;
      drawn.shades.push({ mesh, opaque, faded: palette.toon(base, FADED) });
      palette.outline(mesh);
      drawn.inks.push(...mesh.getChildMeshes(true).filter((c) => c.name === `${mesh.name} ink`));
    }
  }
  return drawn;
}

/** The toon base for a glTF material: its texture and colour as authored. */
function baseOf(source: Material): { texture: Texture | null; color: Color3 } {
  const pbr = source as Partial<Pick<PBRMaterial, 'albedoTexture' | 'albedoColor'>>;
  return {
    texture: pbr.albedoTexture instanceof Texture ? pbr.albedoTexture : null,
    color: pbr.albedoColor ?? Color3.White(),
  };
}

/** The props, each on the ground under it: a kit model's copy at each point, or one stand-in per model key drawn as thin instances. */
async function drawProps(
  scene: Scene,
  models: Models,
  props: readonly Prop[],
  t: Terrain,
  palette: Palette,
  registry: Readonly<Record<string, KitModel>>,
  root: TransformNode,
): Promise<void> {
  const byModel = new Map<string, Prop[]>();
  for (const p of props) {
    byModel.set(p.model, [...(byModel.get(p.model) ?? []), p]);
  }
  for (const [key, points] of byModel) {
    const kit = registry[key];
    if (kit) {
      for (const p of points) {
        const d = await placeKit(
          scene,
          models,
          kit,
          key,
          p.x,
          p.z,
          p.yaw,
          height(t, p.x, p.z),
          palette,
        );
        d.node.parent = root;
      }
      continue;
    }
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
