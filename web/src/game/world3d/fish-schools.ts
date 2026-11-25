// A sea's fish (underground-and-underwater.md section 7): schools of a dozen on slow loops 4–8 m over the floor round the player, clear of the camera's line.
import type { Vector3 } from '@babylonjs/core/Maths/math.vector';
import '@babylonjs/core/Meshes/thinInstanceMesh'; // the thin instance methods on Mesh
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Models } from './models';
import { baseOf } from './place';
import type { Palette } from './toon';

const FISH_FILE = 'sea/fish.glb'; // 0.3 m long, facing +z
const SCHOOLS = 3;
const FISH = 12; // in each school
const TILE_M = 48; // the schools loop in a square this wide round the player, each wrapping to its far side as the player goes
const EDGE_M = 4; // a school shrinks to nothing over this much before the square's edge, so none pops
const LOOP_M = [4, 7] as const; // the narrowest and widest a school's loop is, as a radius
const UP_M = [4.8, 7.2] as const; // how high over the floor it loops, bobbing BOB_M and each fish RISE_M either way: 4–8 m
const BOB_M = 0.4;
const BOB_RATE = 0.3; // radians a second
const SPEED_M_S = 1.2; // along its loop
const ARC = 1.4; // the radians of its loop a school's fish string out along
const STRAY_M = 0.6; // how far a fish keeps out or in from its school's line
const RISE_M = 0.3; // and above or below it
const CLEAR_M = 0.6; // a fish shrinks to nothing as it comes this near the camera's line to the chest
const FADE_M = 1.5; // from this much further off
const R2 = [0.7548776662466927, 0.5698402909980532]; // the plastic number's steps, spreading the schools over the square
const SAMPLES = 32; // round a loop, inside, along and outside its line, for the highest floor under it
const LOW_M = UP_M[0] - BOB_M - RISE_M; // the least a fish swims over the floor right under it

/** The region's extent, which each school's whole loop keeps within. */
export interface Seabed {
  readonly size_x: number;
  readonly size_z: number;
}

/** One fish's place in its school: how far behind its lead along the loop, and off its line. */
interface Fish {
  readonly behind: number;
  readonly out: number;
  readonly rise: number;
}

/** A school: where its loop's middle stands in the first square, how wide and high it loops and which way, and its fish. */
interface School {
  readonly x: number;
  readonly z: number;
  readonly radius: number;
  readonly up: number;
  readonly way: 1 | -1;
  readonly phase: number;
  readonly fish: readonly Fish[];
}

/** v kept margin inside 0 to size, or size's middle if it's narrower than two margins. */
function within(v: number, margin: number, size: number): number {
  return size < 2 * margin ? size / 2 : Math.min(size - margin, Math.max(margin, v));
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** A fraction from i's place in a fixed sequence: the same schools in every sea. */
const spread = (i: number) => (i * 0.618034 + 0.31) % 1;

/** How far (px, py, pz) is from the segment from a to b. */
function offSegment(px: number, py: number, pz: number, a: Vector3, b: Vector3): number {
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const dz = b.z - a.z;
  const len = dx * dx + dy * dy + dz * dz;
  const t = len > 0 ? clamp01(((px - a.x) * dx + (py - a.y) * dy + (pz - a.z) * dz) / len) : 0;
  return Math.hypot(px - (a.x + dx * t), py - (a.y + dy * t), pz - (a.z + dz * t));
}

/** The fish of a sea, drawn round the camera's target once their model is in. */
export class FishSchools {
  private readonly matrices = new Float32Array(16 * SCHOOLS * FISH);
  private meshes: readonly Mesh[] = [];
  private readonly schools: readonly School[];
  private readonly tops = new Float64Array(3 * SCHOOLS).fill(NaN); // each school's last loop middle, x and z, and the highest floor under that loop

  constructor(
    scene: Scene,
    palette: Palette,
    models: Models,
    private readonly seabed: Seabed,
  ) {
    this.schools = Array.from({ length: SCHOOLS }, (_, s) => ({
      x: TILE_M * ((0.5 + (s + 1) * R2[0]!) % 1),
      z: TILE_M * ((0.5 + (s + 1) * R2[1]!) % 1),
      radius: LOOP_M[0] + (LOOP_M[1] - LOOP_M[0]) * spread(s),
      up: UP_M[0] + (UP_M[1] - UP_M[0]) * spread(s + 7),
      way: s % 2 === 0 ? 1 : -1,
      phase: s * 2.1,
      fish: Array.from({ length: FISH }, (_, f) => ({
        behind: (ARC * f) / FISH,
        out: STRAY_M * (2 * spread(s * FISH + f) - 1),
        rise: RISE_M * (2 * spread(s * FISH + f + 5) - 1),
      })),
    }));
    models
      .load(FISH_FILE)
      .then((container) => {
        if (scene.isDisposed) {
          return; // its scene went while it loaded
        }
        const made = container.instantiateModelsToScene((n) => `fish ${n}`, false, {
          doNotInstantiate: true,
        });
        for (const a of made.animationGroups) {
          a.dispose();
        }
        const meshes = made.rootNodes
          .flatMap((r) => r.getChildMeshes(false))
          .filter((m): m is Mesh => m instanceof Mesh && m.material !== null);
        const worlds = meshes.map((m) => m.computeWorldMatrix(true).clone());
        meshes.forEach((mesh, i) => {
          mesh.parent = null;
          mesh.bakeTransformIntoVertices(worlds[i]!);
          mesh.position.setAll(0);
          mesh.rotationQuaternion = null;
          mesh.rotation.setAll(0);
          mesh.scaling.setAll(1);
          mesh.material = palette.toon(baseOf(mesh.material!));
          mesh.isPickable = false;
          mesh.alwaysSelectAsActiveMesh = true;
          mesh.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
        });
        for (const r of made.rootNodes) {
          r.dispose(false, false);
        }
        this.meshes = meshes;
      })
      .catch((err: unknown) => {
        console.info(`finga: ${FISH_FILE} didn't load: ${String(err)}`); // the sea goes on without them
      });
  }

  /** The highest floor under school i's loop round (cx, cz), sampled again only when that middle moves. */
  private top(i: number, cx: number, cz: number, floor: (x: number, z: number) => number): number {
    const t = this.tops;
    if (t[3 * i] === cx && t[3 * i + 1] === cz) {
      return t[3 * i + 2]!;
    }
    const s = this.schools[i]!;
    let top = floor(cx, cz);
    for (let k = 0; k < SAMPLES; k++) {
      const a = (2 * Math.PI * k) / SAMPLES;
      for (const r of [s.radius - STRAY_M, s.radius, s.radius + STRAY_M]) {
        top = Math.max(top, floor(cx + r * Math.cos(a), cz + r * Math.sin(a)));
      }
    }
    t[3 * i] = cx;
    t[3 * i + 1] = cz;
    t[3 * i + 2] = top;
    return top;
  }

  /** Lays the fish out at seconds round chest, which the camera at eye follows, over floor: turned along their loops, shrunk near the square's edge and the line from eye to chest. */
  frame(
    seconds: number,
    eye: Vector3,
    chest: Vector3,
    floor: (x: number, z: number) => number,
  ): void {
    const m = this.matrices;
    let o = 0;
    for (let i = 0; i < SCHOOLS; i++) {
      const s = this.schools[i]!;
      const wx = s.x - TILE_M * Math.round((s.x - chest.x) / TILE_M); // its loop's middle in the square round chest, the same number while the square stands
      const wz = s.z - TILE_M * Math.round((s.z - chest.z) / TILE_M);
      const edge = clamp01(
        Math.min(TILE_M / 2 - Math.abs(wx - chest.x), TILE_M / 2 - Math.abs(wz - chest.z)) / EDGE_M,
      );
      const cx = within(wx, s.radius + STRAY_M, this.seabed.size_x);
      const cz = within(wz, s.radius + STRAY_M, this.seabed.size_z);
      const y = this.top(i, cx, cz, floor) + s.up + BOB_M * Math.sin(BOB_RATE * seconds + s.phase);
      const lead = s.phase + (s.way * SPEED_M_S * seconds) / s.radius;
      for (const f of s.fish) {
        const a = lead - s.way * f.behind;
        const r = s.radius + f.out;
        const x = cx + r * Math.cos(a);
        const z = cz + r * Math.sin(a);
        const fy = Math.max(y + f.rise, floor(x, z) + LOW_M); // over a peak between the samples too
        const k = Math.min(edge, clamp01((offSegment(x, fy, z, eye, chest) - CLEAR_M) / FADE_M));
        const vx = -s.way * Math.sin(a); // along its loop, the way it swims
        const vz = s.way * Math.cos(a);
        m[o] = k * vz; // turned so its +z runs along (vx, vz)
        m[o + 1] = 0;
        m[o + 2] = -k * vx;
        m[o + 3] = 0;
        m[o + 4] = 0;
        m[o + 5] = k;
        m[o + 6] = 0;
        m[o + 7] = 0;
        m[o + 8] = k * vx;
        m[o + 9] = 0;
        m[o + 10] = k * vz;
        m[o + 11] = 0;
        m[o + 12] = x;
        m[o + 13] = fy;
        m[o + 14] = z;
        m[o + 15] = 1;
        o += 16;
      }
    }
    for (const mesh of this.meshes) {
      mesh.thinInstanceBufferUpdated('matrix');
    }
  }
}
