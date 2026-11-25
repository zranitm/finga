// A sea's light rays (underground-and-underwater.md section 7): a few long, faint, additive cards slanting down from the sun, drifting slowly round the player, never across the camera's line to their chest.
import type { Vector3 } from '@babylonjs/core/Maths/math.vector';
import { CreatePlane } from '@babylonjs/core/Meshes/Builders/planeBuilder';
import '@babylonjs/core/Meshes/thinInstanceMesh'; // the thin instance methods on Mesh
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { CHEST_M } from './follow';
import { SEA_SUN } from './sealight';
import { GLOW_ORDER } from './see-through';
import type { Palette } from './toon';

const RAYS = 7;
const TILE_M = 48; // the rays stand in a square this wide round the player, each wrapping to its far side as it drifts out
const LENGTH_M = 24; // a ray's length up the sun's way, from its foot
const WIDTH_M = [1.6, 3.4] as const; // the narrowest and widest a ray is
const DRIFT_M_S = 0.25; // how fast each drifts
const CLEAR_M = 1; // the gap a ray keeps from the camera's line to the chest, narrowing to nothing as it comes nearer
const EDGE_M = 4; // a ray narrows to nothing over this much before the square's edge, so none pops
const SINK_M = 2; // a ray's foot under the player's feet, where it rises from nothing
const BREATH = 0.15; // the share of its width a ray swells and thins by, slowly
const BREATH_RATE = 0.4; // radians a second
const R2 = [0.7548776662466927, 0.5698402909980532]; // the plastic number's steps, spreading the rays evenly over the square

/** The sun's way up, a unit vector: each ray runs along it. */
const UP = SEA_SUN.normalizeToNew().scaleInPlace(-1);

/** One ray: where it stands in the world at its start, its way and width, and its breath's phase. */
interface Ray {
  readonly x: number;
  readonly z: number;
  readonly dx: number;
  readonly dz: number;
  readonly width: number;
  readonly phase: number;
}

/** v held within half of span either side of 0, as a ray wraps round the square. */
function wrapped(v: number, span: number): number {
  return v - span * Math.round(v / span);
}

/** The rays of a sea, drawn round the camera's target. */
export class SeaRays {
  private readonly card: Mesh;
  private readonly matrices = new Float32Array(16 * RAYS);
  private readonly rays: readonly Ray[];

  constructor(scene: Scene, palette: Palette) {
    this.card = CreatePlane('sea rays', { size: 1 }, scene);
    this.card.material = palette.ray();
    this.card.alphaIndex = GLOW_ORDER;
    this.card.isPickable = false;
    this.card.alwaysSelectAsActiveMesh = true;
    this.card.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
    this.rays = Array.from({ length: RAYS }, (_, i) => {
      const spread = (i * 0.618034) % 1;
      const way = 0.4 + (spread - 0.5); // all drift much the same way, as the water above moves
      return {
        x: TILE_M * ((0.5 + (i + 1) * R2[0]!) % 1),
        z: TILE_M * ((0.5 + (i + 1) * R2[1]!) % 1),
        dx: DRIFT_M_S * Math.cos(way),
        dz: DRIFT_M_S * Math.sin(way),
        width: WIDTH_M[0] + (WIDTH_M[1] - WIDTH_M[0]) * spread,
        phase: i * 1.7,
      };
    });
  }

  /** Lays the rays out at seconds round chest, the point the camera at eye follows: each turned about its length to the camera, narrowed near the square's edge and near the line from eye to chest. */
  frame(seconds: number, eye: Vector3, chest: Vector3): void {
    const footY = chest.y - CHEST_M - SINK_M;
    const m = this.matrices;
    for (let i = 0; i < RAYS; i++) {
      const r = this.rays[i]!;
      const ox = wrapped(r.x + r.dx * seconds - chest.x, TILE_M);
      const oz = wrapped(r.z + r.dz * seconds - chest.z, TILE_M);
      const fx = chest.x + ox;
      const fz = chest.z + oz;
      const half =
        (r.width / 2) * (1 - BREATH + BREATH * Math.sin(BREATH_RATE * seconds + r.phase));
      const edge = Math.min(TILE_M / 2 - Math.abs(ox), TILE_M / 2 - Math.abs(oz)) / EDGE_M;
      const tx = fx + UP.x * LENGTH_M;
      const ty = footY + UP.y * LENGTH_M;
      const tz = fz + UP.z * LENGTH_M;
      const gap = segmentGap(
        fx,
        footY,
        fz,
        tx,
        ty,
        tz,
        eye.x,
        eye.y,
        eye.z,
        chest.x,
        chest.y,
        chest.z,
      );
      const w = 2 * half * clamp01(Math.min(edge, (gap - CLEAR_M) / half));
      const mx = (fx + tx) / 2;
      const my = (footY + ty) / 2;
      const mz = (fz + tz) / 2;
      let ex = eye.x - mx; // toward the camera, square to the ray
      let ey = eye.y - my;
      let ez = eye.z - mz;
      const along = ex * UP.x + ey * UP.y + ez * UP.z;
      ex -= along * UP.x;
      ey -= along * UP.y;
      ez -= along * UP.z;
      const n = Math.hypot(ex, ey, ez) || 1;
      ex /= n;
      ey /= n;
      ez /= n;
      const sx = UP.y * ez - UP.z * ey; // across it: its way up crossed with the camera's way
      const sy = UP.z * ex - UP.x * ez;
      const sz = UP.x * ey - UP.y * ex;
      const o = 16 * i;
      m[o] = sx * w;
      m[o + 1] = sy * w;
      m[o + 2] = sz * w;
      m[o + 3] = 0;
      m[o + 4] = UP.x * LENGTH_M;
      m[o + 5] = UP.y * LENGTH_M;
      m[o + 6] = UP.z * LENGTH_M;
      m[o + 7] = 0;
      m[o + 8] = ex;
      m[o + 9] = ey;
      m[o + 10] = ez;
      m[o + 11] = 0;
      m[o + 12] = mx;
      m[o + 13] = my;
      m[o + 14] = mz;
      m[o + 15] = 1;
    }
    this.card.thinInstanceBufferUpdated('matrix');
  }
}

function clamp01(v: number): number {
  return Math.min(1, Math.max(0, v));
}

/** The least distance between the segments from p1 to q1 and from p2 to q2 (Ericson, Real-Time Collision Detection, 5.1.9). */
function segmentGap(
  p1x: number,
  p1y: number,
  p1z: number,
  q1x: number,
  q1y: number,
  q1z: number,
  p2x: number,
  p2y: number,
  p2z: number,
  q2x: number,
  q2y: number,
  q2z: number,
): number {
  const d1x = q1x - p1x;
  const d1y = q1y - p1y;
  const d1z = q1z - p1z;
  const d2x = q2x - p2x;
  const d2y = q2y - p2y;
  const d2z = q2z - p2z;
  const rx = p1x - p2x;
  const ry = p1y - p2y;
  const rz = p1z - p2z;
  const a = d1x * d1x + d1y * d1y + d1z * d1z;
  const e = d2x * d2x + d2y * d2y + d2z * d2z;
  const f = d2x * rx + d2y * ry + d2z * rz;
  let s = 0;
  let t = 0;
  if (e === 0) {
    s = clamp01(-(d1x * rx + d1y * ry + d1z * rz) / a); // the camera on the chest: a point
  } else {
    const c = d1x * rx + d1y * ry + d1z * rz;
    const b = d1x * d2x + d1y * d2y + d1z * d2z;
    const denom = a * e - b * b;
    s = denom > 0 ? clamp01((b * f - c * e) / denom) : 0;
    t = (b * s + f) / e;
    if (t < 0) {
      t = 0;
      s = clamp01(-c / a);
    } else if (t > 1) {
      t = 1;
      s = clamp01((b - c) / a);
    }
  }
  return Math.hypot(
    p1x + d1x * s - (p2x + d2x * t),
    p1y + d1y * s - (p2y + d2y * t),
    p1z + d1z * s - (p2z + d2z * t),
  );
}
