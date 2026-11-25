// A region drawn (large-regions.md section 10): the overview under everything, its 64 m chunks streamed round the camera's target at three details, each chunk's props as thin instances per model, and each chunk's ground handed to the sim as it comes.
import type { Material } from '@babylonjs/core/Materials/material';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Matrix } from '@babylonjs/core/Maths/math.vector';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import { Scene } from '@babylonjs/core/scene';

import { backoff } from '@/lib/backoff';
import { height } from '@/sim/terrain';

import type { Lighting } from './body';
import type { BuiltChunk, ChunkSource } from './chunk-build';
import {
  DETAILS,
  linearPalette,
  overviewGround,
  overviewIndices,
  overviewSkirts,
} from './ground-mesh';
import type { GroundArrays } from './ground-mesh';
import { kitModels } from './kit-models';
import type { RegionMap } from './map';
import type { Models } from './models';
import { kitParts, placement, skyFor } from './place';
import type { KitPart, Look, Place } from './place';
import { bakeUrl, chunkFile } from './region-bake';
import type { Overview, Region } from './region-bake';
import type { RegionGround } from './region-ground';
import { drawWater } from './water';
import { WaypointStones } from './waypoint-stones';

const LOAD_M = 256; // chunks within this of the camera's target load, nearest first
const FREE_M = 320; // and go past this
const DETAIL_M = [64, 128] as const; // every sample within the first, every 2nd within the second, every 4th beyond
const PROPS_M = 160; // a chunk's props show within this; the fog hides them past it
const ASKED = 4; // chunks asked at once, past those the veil waits on, which all go together
const MADE = 2; // chunk meshes made a frame, so streaming never holds one up
const READY_M = 128; // the chunks within this of an arrival are drawn before the veil lifts
const TRIES = 5; // a chunk that fails this often breaks the region
const RETRY = { baseMs: 500, capMs: 8000, jitter: 'equal' } as const; // and waits this backoff before each try again: five tries break 3.75–7.5 s after the first fails

/** What a region tells the world as its chunks come, and the clock its retries wait on. */
export interface RegionHooks {
  /** A chunk's ground came. */
  readonly landed: () => void;
  /** A chunk failed every try: the region can't be walked. */
  readonly broke: (chunk: string) => void;
  readonly now: () => number;
}

/** How far (x, z) is from chunk (cx, cz)'s square, side metres: 0 inside it. */
export function chunkDistance(cx: number, cz: number, side: number, x: number, z: number): number {
  const dx = Math.max(cx * side - x, 0, x - (cx + 1) * side);
  const dz = Math.max(cz * side - z, 0, z - (cz + 1) * side);
  return Math.hypot(dx, dz);
}

/** The detail a chunk d metres from the target is drawn at, as an index into DETAILS. */
export function detailAt(d: number): number {
  return d < DETAIL_M[0] ? 0 : d < DETAIL_M[1] ? 1 : 2;
}

/** The chunks of an ncx by ncz region of side-metre chunks within reach of (x, z), nearest first. */
export function chunksNear(
  ncx: number,
  ncz: number,
  side: number,
  x: number,
  z: number,
  reach: number,
): { cx: number; cz: number; d: number }[] {
  const out: { cx: number; cz: number; d: number }[] = [];
  const span = (v: number, n: number) => [
    Math.max(Math.floor((v - reach) / side), 0),
    Math.min(Math.floor((v + reach) / side), n - 1),
  ];
  const [x0, x1] = span(x, ncx);
  const [z0, z1] = span(z, ncz);
  for (let cz = z0!; cz <= z1!; cz++) {
    for (let cx = x0!; cx <= x1!; cx++) {
      const d = chunkDistance(cx, cz, side, x, z);
      if (d <= reach) {
        out.push({ cx, cz, d });
      }
    }
  }
  return out.sort((a, b) => a.d - b.d || a.cz - b.cz || a.cx - b.cx);
}

/** Draws region in scene: its sky, the overview, and the chunks round the camera's target as follow is told it, each building on chunks; each chunk's ground goes to the region's as it comes, as hooks hear. */
export function buildRegion(
  scene: Scene,
  region: RegionMap,
  lighting: Lighting,
  models: Models,
  chunks: ChunkSource,
  hooks: RegionHooks,
): Place {
  skyFor(scene, region.region.sky);
  return new RegionView(scene, region, lighting, models, chunks, hooks);
}

/** A chunk drawn: its ground at a detail, and its props, which come as their models load. */
interface Drawn {
  readonly built: BuiltChunk;
  readonly ground: Mesh;
  readonly props: TransformNode;
  detail: number;
}

class RegionView implements Place {
  readonly looks: readonly Look[]; // its waypoint stones'
  private readonly root: TransformNode;
  private readonly templates: TransformNode;
  private readonly material: Material;
  private readonly linear: Float32Array;
  private readonly coarsest: number;
  private readonly overview: Mesh;
  private readonly overviewArrays: Omit<GroundArrays, 'details'>;
  private skirts: Mesh | null = null; // round the overview's holes
  private readonly drawn = new Map<string, Drawn>();
  private readonly asked = new Set<string>();
  private readonly failed = new Map<string, { tries: number; after: number }>(); // a chunk that failed: how often, and when it may be asked again
  private readonly kits = new Map<string, Promise<KitPart[] | null>>();
  private toMake: BuiltChunk[] = []; // built, to be made a few a frame
  private holes = false; // whether the overview's holes need cutting again
  private gone = false;
  private readonly region: Region;
  private readonly far: Overview;
  private readonly ground: RegionGround;
  private readonly stones: WaypointStones;

  constructor(
    private readonly scene: Scene,
    map: RegionMap,
    private readonly lighting: Lighting,
    private readonly models: Models,
    private readonly chunks: ChunkSource,
    private readonly hooks: RegionHooks,
  ) {
    const { region, overview: far } = map;
    this.region = region;
    this.far = far;
    this.ground = map.ground;
    this.stones = new WaypointStones(
      scene,
      map.waypoints,
      lighting.palette,
      models,
      hooks.now,
      (x, z) => height(map.ground.geometry.terrain, x, z),
    );
    this.looks = this.stones.looks;
    this.root = new TransformNode(`region ${region.ref.id}`, scene);
    this.templates = new TransformNode('kit models', scene);
    this.templates.parent = this.root;
    this.templates.setEnabled(false); // each chunk draws its own copies
    this.material = lighting.palette.toon(
      { texture: null, color: Color3.White(), vertexColors: true },
      1,
      true,
    );
    this.linear = linearPalette(region.palette);
    this.coarsest = Math.max(DETAILS[2], far.spacing / region.cell);
    this.overviewArrays = overviewGround(far, this.linear);
    this.overview = this.mesh('overview', this.overviewArrays, this.overviewHoles());
    this.overview.parent = this.root;
    drawWater(scene, region, lighting.palette, this.root);
  }

  follow(x: number, z: number): void {
    const side = this.region.chunk;
    for (const [k, d] of this.drawn) {
      if (chunkDistance(d.built.cx, d.built.cz, side, x, z) > FREE_M) {
        d.ground.dispose(false, false);
        d.props.dispose(false, false);
        this.drawn.delete(k);
        this.holes = true;
      }
    }
    for (const k of this.asked) {
      const [cx, cz] = k.split('_').map(Number);
      if (chunkDistance(cx!, cz!, side, x, z) > FREE_M) {
        this.asked.delete(k); // dropped as it comes
      }
    }
    this.toMake = this.toMake.filter((b) => chunkDistance(b.cx, b.cz, side, x, z) <= FREE_M);
    for (const b of this.toMake.splice(0, MADE)) {
      this.make(b);
    }
    for (const d of this.drawn.values()) {
      const away = chunkDistance(d.built.cx, d.built.cz, side, x, z);
      const detail = detailAt(away);
      if (detail !== d.detail) {
        d.detail = detail;
        d.ground.setIndices(d.built.ground.details[detail]!);
      }
      d.props.setEnabled(away <= PROPS_M);
    }
    const [ncx, ncz] = this.region.chunks;
    const now = this.hooks.now();
    for (const { cx, cz, d } of chunksNear(ncx, ncz, side, x, z, LOAD_M)) {
      if (this.asked.size >= ASKED && d > READY_M) {
        break;
      }
      const k = key(cx, cz);
      const failed = this.failed.get(k);
      if (
        !this.drawn.has(k) &&
        !this.asked.has(k) &&
        !(failed && (failed.tries >= TRIES || now < failed.after)) &&
        !this.toMake.some((b) => b.cx === cx && b.cz === cz)
      ) {
        this.ask(cx, cz);
      }
    }
    if (this.holes) {
      this.holes = false;
      this.overview.setIndices(this.overviewHoles());
      this.skirts?.dispose(false, false);
      const per = this.region.chunk / this.far.spacing;
      const s = overviewSkirts(
        this.far,
        this.overviewArrays,
        per,
        (cx, cz) => this.drawn.get(key(cx, cz))?.built.ground.skirt,
      );
      this.skirts = this.mesh('overview skirts', s, s.indices);
      this.skirts.parent = this.root;
    }
  }

  ready(x: number, z: number): boolean {
    const [ncx, ncz] = this.region.chunks;
    return chunksNear(ncx, ncz, this.region.chunk, x, z, READY_M).every(({ cx, cz }) =>
      this.drawn.has(key(cx, cz)),
    );
  }

  dispose(): void {
    this.gone = true;
    this.stones.dispose();
    this.root.dispose(false, false);
    this.scene.fogMode = Scene.FOGMODE_NONE;
  }

  /** Asks for chunk (cx, cz); its ground goes to the sim as it comes, and it's made a later frame, unless it went out of reach meanwhile. One that fails, or whose ground doesn't fit the region, is asked again after a backoff, the overview standing in meanwhile, and breaks the region once it has failed TRIES times. */
  private ask(cx: number, cz: number): void {
    const k = key(cx, cz);
    this.asked.add(k);
    this.chunks
      .build({
        url: bakeUrl(this.region.ref, chunkFile(cx, cz)),
        cell: this.region.cell,
        linear: this.linear,
        coarsest: this.coarsest,
      })
      .then(
        (b) => {
          if (this.gone) {
            return;
          }
          try {
            this.ground.add(b.sim);
          } catch (err) {
            this.fail(k, err);
            return;
          }
          this.failed.delete(k);
          this.hooks.landed();
          if (this.asked.delete(k)) {
            this.toMake.push(b);
          }
        },
        (err: unknown) => {
          if (!this.gone) {
            this.fail(k, err);
          }
        },
      );
  }

  private fail(k: string, err: unknown): void {
    this.asked.delete(k);
    const tries = (this.failed.get(k)?.tries ?? 0) + 1;
    this.failed.set(k, { tries, after: this.hooks.now() + backoff(tries - 1, RETRY) });
    console.info(
      `finga: chunk ${k} of ${this.region.ref.id} didn't load, try ${tries} of ${TRIES}: ${err instanceof Error ? err.message : String(err)}`,
    );
    if (tries >= TRIES) {
      this.hooks.broke(k);
    }
  }

  /** Draws a built chunk: its ground at the farthest detail till the next follow sets it, and its props as their models load. */
  private make(b: BuiltChunk): void {
    const k = key(b.cx, b.cz);
    const detail = DETAILS.length - 1;
    const ground = this.mesh(`chunk ${k}`, b.ground, b.ground.details[detail]!);
    ground.parent = this.root;
    ground.position.set(b.cx * this.region.chunk, 0, b.cz * this.region.chunk);
    ground.freezeWorldMatrix();
    const props = new TransformNode(`chunk ${k} props`, this.scene);
    props.parent = this.root;
    this.drawn.set(k, { built: b, ground, props, detail });
    this.holes = true;
    const byModel = new Map<number, Matrix[]>();
    b.models.forEach((m, i) => {
      const kit = kitModels[this.region.models[m]!];
      if (kit) {
        const [x, y, z, yaw] = b.props.subarray(4 * i, 4 * i + 4);
        byModel.set(m, [...(byModel.get(m) ?? []), placement(kit, x!, z!, yaw!, y!)]);
      }
    });
    for (const [m, at] of byModel) {
      void this.kit(this.region.models[m]!).then((parts) => {
        if (!parts || props.isDisposed()) {
          return;
        }
        for (const part of parts) {
          const body = part.mesh.clone(`${part.mesh.name} ${k}`, props); // with its ink, as drawn
          body.makeGeometryUnique(); // the chunk's own: Babylon keeps thin instances' matrices on the geometry, which each mesh sharing it draws
          const ink = body.getChildMeshes(true);
          for (const mesh of [body, ...ink] as Mesh[]) {
            if (mesh !== body) {
              body.geometry!.applyToMesh(mesh);
            }
            mesh.setEnabled(true);
            mesh.thinInstanceAdd(at); // which grows its bounds round them, for culling
            mesh.freezeWorldMatrix();
          }
        }
      });
    }
  }

  /** key's kit model, loaded once for the region: its parts kept hidden, for each chunk to copy; null if it won't load. */
  private kit(key: string): Promise<KitPart[] | null> {
    let parts = this.kits.get(key);
    if (!parts) {
      parts = kitParts(
        this.models,
        kitModels[key]!,
        key,
        this.lighting.palette,
        null,
        this.templates,
      ).catch((err: unknown) => {
        console.info(
          `finga: kit model ${key} didn't load: ${err instanceof Error ? err.message : String(err)}`,
        );
        return null;
      });
      this.kits.set(key, parts);
    }
    return parts;
  }

  /** The overview's triangles but under the chunks drawn. */
  private overviewHoles(): Uint32Array {
    const per = this.region.chunk / this.far.spacing;
    return overviewIndices(this.far, per, (cx, cz) => this.drawn.has(key(cx, cz)));
  }

  /** A ground mesh from a, drawn with indices. */
  private mesh(name: string, a: Omit<GroundArrays, 'details'>, indices: Uint32Array): Mesh {
    const data = new VertexData();
    data.positions = a.positions;
    data.normals = a.normals;
    data.colors = a.colors;
    data.indices = indices;
    const m = new Mesh(name, this.scene);
    data.applyToMesh(m, true);
    m.material = this.material;
    m.receiveShadows = true;
    m.isPickable = false;
    return m;
  }
}

function key(cx: number, cz: number): string {
  return `${cx}_${cz}`;
}
