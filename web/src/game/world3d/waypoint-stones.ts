// A region's waypoint stones (large-regions.md section 8's Look): dim till found and lit after, by the found set Welcome and WaypointFound give, and a find flares its stone.
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { Constants } from '@babylonjs/core/Engines/constants';
import type { Material } from '@babylonjs/core/Materials/material';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Matrix } from '@babylonjs/core/Maths/math.vector';
import { CreateCylinder } from '@babylonjs/core/Meshes/Builders/cylinderBuilder';
import { CreatePlane } from '@babylonjs/core/Meshes/Builders/planeBuilder';
import { CreateTorus } from '@babylonjs/core/Meshes/Builders/torusBuilder';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Observer } from '@babylonjs/core/Misc/observable';
import type { Scene } from '@babylonjs/core/scene';

import type { MapWaypoint } from '@/state/map-view';
import { waypoints as foundWaypoints } from '@/state/waypoints';

import type { Models } from './models';
import { kitParts, placement } from './place';
import type { KitPart } from './place';
import { linearOf } from './toon';
import type { Palette } from './toon';

const KIT = { file: 'waypoint/waypoint_stone.glb', scale: 1, y: 0 };
const YAW = 90; // a rune face toward the camera, which looks north
const DIM = 0x1a8c93; // an unfound crystal
const BRIGHT = 0x9ff8ff; // a found one, toward white
const RUNE = 0x00dce6; // a found stone's runes, the map's waypoint cyan
const RUNE_DARK = 0x3e5658; // an unfound stone's, carved but unlit
const CRYSTAL_Y = 3.24; // the crystal's middle over the ground
const DOT_PX = 64;

/** The halo's colour, a touch bluer than the crystal's cyan, drawn over what's behind it rather than added to it, so it reads cyan on grass as on dark ground. */
const HALO = 0x00d0f4;
/** Each halo's width in metres and its alpha: a faint one is small rather than thin, as a thin cyan over grass reads mint. */
const HALOS = { unfound: { size: 1.35, alpha: 0.7 }, found: { size: 2.2, alpha: 0.9 } };
/** The halo's dot, as shares of its radius: clearer inside HOLE, where the crystal shows through, solid out to CORE, then a short fade, as the fade from cyan to clear passes through mint. */
const HOLE = 0.2;
const CORE = 0.6;
/** A find's flare in ms: the halo swells and settles and a ring spreads RING_M across over FLARE_MS; a beam rises and is gone by BEAM_MS. */
const FLARE_MS = 800;
const BEAM_MS = 600;
const RING_M = 7;

interface Stone {
  readonly w: MapWaypoint;
  readonly at: Matrix;
  readonly halo: Mesh;
  lit: boolean;
}

const ease = (k: number) => 1 - (1 - Math.min(1, Math.max(0, k))) ** 2;

export class WaypointStones {
  private readonly root: TransformNode;
  private readonly stones: Stone[];
  private readonly dot: RawTexture;
  private readonly halos: { readonly unfound: StandardMaterial; readonly found: StandardMaterial };
  private readonly beam: Mesh;
  private readonly ring: Mesh;
  private parts: { readonly unfound: KitPart[]; readonly found: KitPart[] } | null = null;
  private flaring: { readonly stone: Stone; readonly from: number } | null = null; // one at a time: a newer find takes it over
  private readonly off: () => void;
  private readonly frame: Observer<Scene>;

  constructor(
    private readonly scene: Scene,
    waypoints: readonly MapWaypoint[],
    palette: Palette,
    models: Models,
    private readonly now: () => number,
  ) {
    this.root = new TransformNode('waypoint stones', scene);
    this.dot = dotTexture(scene);
    this.halos = {
      unfound: this.glow('waypoint halo', HALOS.unfound.alpha, true),
      found: this.glow('waypoint halo found', HALOS.found.alpha, true),
    };
    const found = foundWaypoints.getState().found;
    this.stones = waypoints.map((w) => {
      const halo = CreatePlane(`waypoint ${w.id} halo`, { size: 1 }, scene);
      halo.billboardMode = Mesh.BILLBOARDMODE_ALL;
      halo.position.set(w.x, w.y + CRYSTAL_Y, w.z);
      const stone = { w, at: placement(KIT, w.x, w.z, YAW, w.y), halo, lit: found.has(w.id) };
      this.haloOf(stone, 0);
      return stone;
    });
    this.beam = CreateCylinder(
      'waypoint beam',
      { height: 9, diameterTop: 0.15, diameterBottom: 0.7, tessellation: 24 },
      scene,
    );
    this.beam.material = this.glow('waypoint beam', 0.7, false);
    this.ring = CreateTorus(
      'waypoint ring',
      { diameter: 1, thickness: 0.05, tessellation: 64 },
      scene,
    );
    this.ring.material = this.glow('waypoint ring', 0.95, false);
    for (const m of [...this.stones.map((s) => s.halo), this.beam, this.ring]) {
      m.parent = this.root;
      m.isPickable = false;
    }
    for (const m of [this.beam, this.ring]) {
      m.setEnabled(false);
      m.material!.forceCompilation(m); // so a find doesn't stall a frame
    }
    void this.load(models, palette);
    this.off = foundWaypoints.subscribe((s, before) => {
      this.relight(s.found);
      if (s.last !== null && s.last !== before.last) {
        this.flare(s.last);
      }
    });
    this.frame = scene.onBeforeRenderObservable.add(() => this.step(this.now()));
  }

  dispose(): void {
    this.off();
    this.scene.onBeforeRenderObservable.remove(this.frame);
    for (const m of [this.beam, this.ring]) {
      m.material?.dispose();
    }
    this.root.dispose(false, false); // the stones' toon and glow materials are the palette's, shared
    this.halos.unfound.dispose();
    this.halos.found.dispose();
    this.dot.dispose();
  }

  /** The model twice, painted unfound and found, each drawn at the stones in its state. */
  private async load(models: Models, palette: Palette): Promise<void> {
    const [unfound, found] = await Promise.all([
      kitParts(models, KIT, 'waypoint stone', palette, null, this.root), // casting none, as a region's props, till its cascades come
      kitParts(models, KIT, 'waypoint stone found', palette, null, this.root),
    ]);
    if (this.root.isDisposed()) {
      return;
    }
    const paint = (parts: KitPart[], crystal: Color3, rune: Material) => {
      for (const p of parts) {
        if (p.mesh.name.endsWith(' Crystal')) {
          p.mesh.material = palette.glow(crystal, 1);
        } else if (p.mesh.name.endsWith(' Rune')) {
          p.mesh.material = rune;
        }
      }
    };
    paint(
      unfound,
      linearOf(DIM),
      palette.toon({ texture: null, color: linearOf(RUNE_DARK) }, 1, true),
    );
    paint(found, linearOf(BRIGHT), palette.glow(linearOf(RUNE), 1));
    this.parts = { unfound, found };
    this.place();
  }

  /** Each painted copy drawn at every stone in its state, and not at all where none is. */
  private place(): void {
    for (const [lit, parts] of [
      [false, this.parts?.unfound ?? []],
      [true, this.parts?.found ?? []],
    ] as const) {
      const at = this.stones.filter((s) => s.lit === lit);
      const flat = new Float32Array(16 * at.length);
      at.forEach((s, i) => s.at.copyToArray(flat, 16 * i));
      for (const p of parts) {
        p.mesh.setEnabled(at.length > 0); // with no instances a mesh draws once where it stands
        for (const mesh of [p.mesh, p.ink]) {
          if (mesh && at.length > 0) {
            mesh.thinInstanceSetBuffer('matrix', flat, 16, true);
            mesh.thinInstanceRefreshBoundingInfo(false);
          }
        }
      }
    }
  }

  private relight(found: ReadonlySet<string>): void {
    let changed = false;
    for (const s of this.stones) {
      if (s.lit !== found.has(s.w.id)) {
        s.lit = !s.lit;
        this.haloOf(s, 0);
        changed = true;
      }
    }
    if (changed) {
      this.place();
    }
  }

  /** s's halo in its state, swollen by swell, 0 to 1. */
  private haloOf(s: Stone, swell: number): void {
    const size = (s.lit ? HALOS.found : HALOS.unfound).size * (1 + 0.5 * swell);
    s.halo.scaling.setAll(size);
    s.halo.material = s.lit ? this.halos.found : this.halos.unfound;
  }

  private flare(id: string): void {
    const stone = this.stones.find((s) => s.w.id === id);
    if (!stone) {
      return; // another region's
    }
    if (this.flaring) {
      this.haloOf(this.flaring.stone, 0);
    }
    this.flaring = { stone, from: this.now() };
    this.ring.position.set(stone.w.x, stone.w.y + 0.08, stone.w.z);
    this.step(this.flaring.from);
  }

  private step(t: number): void {
    const f = this.flaring;
    if (!f) {
      return;
    }
    const k = t - f.from;
    const on = k < FLARE_MS;
    this.ring.setEnabled(on);
    this.beam.setEnabled(k < BEAM_MS);
    if (!on) {
      this.haloOf(f.stone, 0);
      this.flaring = null;
      return;
    }
    const { x, y, z } = f.stone.w;
    this.haloOf(f.stone, Math.sin((Math.PI * k) / FLARE_MS));
    this.beam.position.set(x, y + CRYSTAL_Y + 4.5 * ease((1.5 * k) / BEAM_MS), z);
    this.beam.material!.alpha = 0.7 * (1 - Math.min(k / BEAM_MS, 1));
    const across = 1.4 + (RING_M - 1.4) * ease(k / FLARE_MS);
    this.ring.scaling.set(across, 1, across);
    this.ring.material!.alpha = 0.95 * (1 - k / FLARE_MS);
  }

  /** An unlit glow in the halo's colour, at alpha over what's behind it and through the fog: a soft dot if dotted. */
  private glow(name: string, alpha: number, dotted: boolean): StandardMaterial {
    const m = new StandardMaterial(name, this.scene);
    m.disableLighting = true;
    m.emissiveColor = Color3.FromInts(HALO >> 16, (HALO >> 8) & 255, HALO & 255); // sRGB, as a standard material reads it
    m.alpha = alpha;
    m.alphaMode = Constants.ALPHA_COMBINE;
    m.fogEnabled = false; // a glow shows through the dark and the fog
    m.backFaceCulling = false;
    m.disableDepthWrite = true;
    if (dotted) {
      m.opacityTexture = this.dot;
    }
    return m;
  }
}

/** The halo's dot, white. */
function dotTexture(scene: Scene): RawTexture {
  const px = new Uint8Array(DOT_PX * DOT_PX * 4);
  const mid = (DOT_PX - 1) / 2;
  for (let y = 0; y < DOT_PX; y++) {
    for (let x = 0; x < DOT_PX; x++) {
      const r = Math.min(1, Math.hypot(x - mid, y - mid) / mid);
      const i = (y * DOT_PX + x) * 4;
      px.fill(255, i, i + 3);
      const a = r < HOLE ? 0.25 + 0.75 * (r / HOLE) ** 2 : r <= CORE ? 1 : (1 - r) / (1 - CORE);
      px[i + 3] = Math.round(255 * a);
    }
  }
  const t = RawTexture.CreateRGBATexture(
    px,
    DOT_PX,
    DOT_PX,
    scene,
    false,
    false,
    Texture.BILINEAR_SAMPLINGMODE,
  );
  t.hasAlpha = true;
  return t;
}
