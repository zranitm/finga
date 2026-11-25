// A region's waypoint stones (large-regions.md section 8's Look): dim till found and lit after, by the found set Welcome and WaypointFound give, and a find flares its stone once it's on screen.
import '@babylonjs/core/Meshes/thinInstanceMesh';
import { Constants } from '@babylonjs/core/Engines/constants';
import type { Material } from '@babylonjs/core/Materials/material';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { RawTexture } from '@babylonjs/core/Materials/Textures/rawTexture';
import { Texture } from '@babylonjs/core/Materials/Textures/texture';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Frustum } from '@babylonjs/core/Maths/math.frustum';
import type { Matrix } from '@babylonjs/core/Maths/math.vector';
import { Quaternion, Vector3 } from '@babylonjs/core/Maths/math.vector';
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
import { kitParts, Look, placement, STEPS } from './place';
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

/** Each halo's width in metres, its alpha, and its sRGB colour in the middle and at the rim, drawn over the ground: a fade from cyan to clear over grass passes through mint, so a bluer edge; an unfound one dim by darkness, not thinness. */
const HALOS = {
  unfound: { size: 1.35, alpha: 0.7, middle: 0x006a8a, rim: 0x00587a },
  found: { size: 2.6, alpha: 0.9, middle: 0x00d8f0, rim: 0x00b4ff },
};
type HaloLook = (typeof HALOS)['found'];
/** The share of a halo's radius drawn solid; past it, its alpha falls evenly to nothing at the rim. */
const CORE = 0.45;
/** Where a halo stands from its crystal's middle, in metres: behind it along the camera's look, north and down, so the crystal draws in front of it. */
const BEHIND = { y: -0.3, z: 0.3 };
/** The flare's beam and ring. */
const FLARE = 0x00d0f4;
/** A find's flare in ms: the halo swells and settles and a ring spreads RING_M across over FLARE_MS; a beam rises and is gone by BEAM_MS. */
const FLARE_MS = 800;
const BEAM_MS = 600;
const RING_M = 7;
/** A find's stone waits this long in ms to be on screen and flare; after, it just shows lit. */
const SEEN_WITHIN_MS = 10_000;

/** A halo's and a flare's beam's share of their alpha at each of their stone's STEPS: gone by its faded step, as a stone fades only between the camera and the player, whom a glow at the stone's own alpha would tint. */
const GLOW_STEPS = [1, 0.3, 0, 0, 0] as const;

/** The box a stone's see-through look takes round its spot, halo and all: as wide as the found halo, up to its top. */
const SEE = { reach: 1.3, top: 4.5 };

interface Stone {
  readonly w: MapWaypoint;
  readonly at: Matrix;
  readonly halo: Mesh;
  readonly look: Look;
  lit: boolean;
  step: number; // its look's step in STEPS: whole at 0
  copies: { readonly unfound: Mesh[]; readonly found: Mesh[] } | null; // drawn see-through while it isn't whole
}

/** A stone and when, by the world's clock: its find, or its flare's start. */
interface Since {
  readonly stone: Stone;
  readonly from: number;
}

const ease = (k: number) => 1 - (1 - Math.min(1, Math.max(0, k))) ** 2;

export class WaypointStones {
  private readonly root: TransformNode;
  private readonly stones: Stone[];
  /** Each stone's see-through look, which fades it out of the camera's way to the player as a tall prop is. */
  readonly looks: readonly Look[];
  private readonly halos: { readonly unfound: StandardMaterial; readonly found: StandardMaterial };
  private readonly beam: Mesh;
  private readonly ring: Mesh;
  private parts: { readonly unfound: KitPart[]; readonly found: KitPart[] } | null = null;
  private flaring: Since | null = null; // one at a time: a newer find takes it over
  private waiting: Since[] = []; // found, still dim till first on screen, oldest first
  private readonly off: () => void;
  private readonly frame: Observer<Scene>;

  constructor(
    private readonly scene: Scene,
    waypoints: readonly MapWaypoint[],
    palette: Palette,
    models: Models,
    private readonly now: () => number,
    private readonly heightAt: (x: number, z: number) => number,
  ) {
    this.root = new TransformNode('waypoint stones', scene);
    this.halos = {
      unfound: this.halo('waypoint halo', HALOS.unfound),
      found: this.halo('waypoint halo found', HALOS.found),
    };
    const found = foundWaypoints.getState().found;
    this.stones = waypoints.map((w) => {
      const halo = CreatePlane(`waypoint ${w.id} halo`, { size: 1 }, scene);
      halo.billboardMode = Mesh.BILLBOARDMODE_ALL;
      halo.position.set(w.x, w.y + CRYSTAL_Y + BEHIND.y, w.z + BEHIND.z);
      const stone: Stone = {
        w,
        at: placement(KIT, w.x, w.z, YAW, w.y),
        halo,
        look: new Look(
          new Vector3(w.x - SEE.reach, w.y, w.z - SEE.reach),
          new Vector3(w.x + SEE.reach, w.y + SEE.top, w.z + SEE.reach),
          (step) => this.see(stone, step),
        ),
        lit: found.has(w.id),
        step: 0,
        copies: null,
      };
      this.haloOf(stone, 0);
      return stone;
    });
    this.looks = this.stones.map((s) => s.look);
    this.beam = CreateCylinder(
      'waypoint beam',
      { height: 9, diameterTop: 0.15, diameterBottom: 0.7, tessellation: 24 },
      scene,
    );
    this.beam.material = this.glow('waypoint beam', FLARE, 0.7);
    this.ring = CreateTorus(
      'waypoint ring',
      { diameter: 1, thickness: 0.05, tessellation: 64 },
      scene,
    );
    this.ring.material = this.glow('waypoint ring', FLARE, 0.95);
    for (const m of [...this.stones.map((s) => s.halo), this.beam, this.ring]) {
      m.parent = this.root;
      m.isPickable = false;
    }
    for (const m of [this.beam, this.ring]) {
      m.setEnabled(false);
      m.material!.forceCompilation(m); // so a find doesn't stall a frame
    }
    this.load(models, palette).catch((err: unknown) => {
      if (!this.root.isDisposed()) {
        console.info(
          `finga: the waypoint stone didn't load: ${err instanceof Error ? err.message : String(err)}`,
        ); // the stones' halos stand without it
      } // else its region went while it loaded: nothing to draw it in
    });
    this.off = foundWaypoints.subscribe((s, before) => {
      if (s.last !== null && s.last !== before.last) {
        const stone = this.stones.find((st) => st.w.id === s.last); // none for another region's
        if (stone) {
          this.waiting.push({ stone, from: this.now() });
        }
      } else {
        this.waiting = []; // a join's list lights every stone it holds, unflared
      }
      this.relight(s.found);
      this.step(this.now());
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
    this.halos.unfound.dispose(false, true);
    this.halos.found.dispose(false, true);
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
    for (const s of this.stones) {
      s.copies = { unfound: this.copied(s, unfound), found: this.copied(s, found) };
    }
    this.place();
  }

  /** parts at s alone, drawn see-through: hidden till its look leaves whole. */
  private copied(s: Stone, parts: readonly KitPart[]): Mesh[] {
    return parts.map((p) => {
      const c = p.mesh.clone(`${p.mesh.name} faded ${s.w.id}`, this.root, true);
      c.rotationQuaternion = new Quaternion();
      s.at.decompose(c.scaling, c.rotationQuaternion, c.position);
      c.setEnabled(false);
      return c;
    });
  }

  /** s at its look's step: whole among the instances, else its see-through copy, gone at the last, its halo gone before it. */
  private see(s: Stone, step: number): void {
    s.step = step;
    s.halo.visibility = GLOW_STEPS[step]!;
    this.place();
  }

  /** Each painted copy drawn at every whole stone in its state, and not at all where none is; a stone not whole draws its see-through copy. */
  private place(): void {
    for (const s of this.stones) {
      const parts = this.parts?.[s.lit ? 'found' : 'unfound'] ?? [];
      for (const lit of [false, true]) {
        s.copies?.[lit ? 'found' : 'unfound'].forEach((c, k) => {
          const shown = lit === s.lit && s.step > 0 && s.step < STEPS.length - 1;
          c.setEnabled(shown);
          if (shown) {
            c.material = parts[k]!.steps[s.step]!;
          }
        });
      }
    }
    for (const [lit, parts] of [
      [false, this.parts?.unfound ?? []],
      [true, this.parts?.found ?? []],
    ] as const) {
      const at = this.stones.filter((s) => s.lit === lit && s.step === 0);
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
      if (s.lit !== (found.has(s.w.id) && !this.waiting.some((w) => w.stone === s))) {
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

  /** Lights each waiting stone whose crystal is on screen, flaring it, the newest taking the flare over, and one unseen past SEEN_WITHIN_MS unflared. */
  private watch(t: number): void {
    const camera = this.scene.activeCamera;
    const planes = camera
      ? Frustum.GetPlanes(camera.getViewMatrix().multiply(camera.getProjectionMatrix()))
      : null;
    const late = (w: Since) => t - w.from >= SEEN_WITHIN_MS;
    const due = this.waiting.filter(
      (w) =>
        late(w) ||
        (planes &&
          Frustum.IsPointInFrustum(
            new Vector3(w.stone.w.x, w.stone.w.y + CRYSTAL_Y, w.stone.w.z),
            planes,
          )),
    );
    if (due.length === 0) {
      return;
    }
    this.waiting = this.waiting.filter((w) => !due.includes(w));
    this.relight(foundWaypoints.getState().found);
    for (const w of due.filter((w) => !late(w))) {
      if (this.flaring) {
        this.haloOf(this.flaring.stone, 0);
      }
      this.flaring = { stone: w.stone, from: t };
      this.ring.position.set(w.stone.w.x, w.stone.w.y, w.stone.w.z);
    }
  }

  private step(t: number): void {
    if (this.waiting.length > 0) {
      this.watch(t);
    }
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
    this.beam.visibility = GLOW_STEPS[f.stone.step]!; // its stone's glow, gone as the stone fades
    const across = 1.4 + (RING_M - 1.4) * ease(k / FLARE_MS);
    this.ring.scaling.set(across, 1, across);
    let top = y; // the ring lies flat, so it rides the highest ground under it, on a slope too
    for (let i = 0; i < 16; i++) {
      const a = (i * Math.PI) / 8;
      top = Math.max(
        top,
        this.heightAt(x + (across / 2) * Math.cos(a), z + (across / 2) * Math.sin(a)),
      );
    }
    this.ring.position.y = top + 0.08;
    this.ring.material!.alpha = 0.95 * (1 - k / FLARE_MS);
  }

  /** An unlit glow in color, sRGB, at alpha over what's behind it and through the fog. */
  private glow(name: string, color: number, alpha: number): StandardMaterial {
    const m = new StandardMaterial(name, this.scene);
    m.disableLighting = true;
    m.emissiveColor = Color3.FromInts(color >> 16, (color >> 8) & 255, color & 255); // sRGB, as a standard material reads it
    m.alpha = alpha;
    m.alphaMode = Constants.ALPHA_COMBINE;
    m.fogEnabled = false; // a glow shows through the dark and the fog
    m.backFaceCulling = false;
    m.disableDepthWrite = true;
    return m;
  }

  /** A halo's glow: its soft dot gives both its colour and its alpha. */
  private halo(name: string, look: HaloLook): StandardMaterial {
    const m = this.glow(name, 0, 1); // black, as the dot's colour adds to it
    m.emissiveTexture = m.opacityTexture = dotTexture(this.scene, look);
    return m;
  }
}

/** A halo's dot, sRGB: the middle colour easing to the rim's across it, at its alpha out to CORE, then fading to clear. */
function dotTexture(scene: Scene, look: HaloLook): RawTexture {
  const px = new Uint8Array(DOT_PX * DOT_PX * 4);
  const mid = (DOT_PX - 1) / 2;
  const middle = [16, 8, 0].map((b) => (look.middle >> b) & 255);
  const rim = [16, 8, 0].map((b) => (look.rim >> b) & 255);
  for (let y = 0; y < DOT_PX; y++) {
    for (let x = 0; x < DOT_PX; x++) {
      const r = Math.min(1, Math.hypot(x - mid, y - mid) / mid);
      const i = (y * DOT_PX + x) * 4;
      for (let c = 0; c < 3; c++) {
        px[i + c] = Math.round(middle[c]! + (rim[c]! - middle[c]!) * r);
      }
      px[i + 3] = Math.round(255 * look.alpha * (r <= CORE ? 1 : (1 - r) / (1 - CORE)));
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
