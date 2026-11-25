// A cave's round shadows (underground-and-underwater.md section 7): with no sun, each body near the camera's target has a soft dark disc on the ground under it, all of them thin instances of one disc.
import { CreateDisc } from '@babylonjs/core/Meshes/Builders/discBuilder';
import '@babylonjs/core/Meshes/thinInstanceMesh'; // the thin instance methods on Mesh
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { height } from '@/sim/terrain';

import { groundUnder } from './map';
import type { WorldMap } from './map';
import { SHADOW_ORDER } from './see-through';
import type { Palette } from './toon';

const SHADOW_M = 25; // a body this far from the camera's target or nearer has one, as others cast the sun's within it on the land
const SPREAD = 1.6; // a disc's radius over its body's
const LIFT_M = 0.04; // over the ground, so it never fights it
const ON_TERRAIN_M = 1e-3; // a body's ground this near the terrain is the terrain, whose slope its disc lies on

/** Each frame's discs: begun round the camera's target, one added for each body, then drawn. */
export class RoundShadows {
  private readonly disc: Mesh;
  private matrices = new Float32Array(16 * 32);
  private count = 0;
  private cx = 0;
  private cz = 0;

  /** The discs of map's bodies, lying on its ground. */
  constructor(
    scene: Scene,
    palette: Palette,
    private readonly map: Pick<WorldMap, 'geometry'>,
  ) {
    this.disc = CreateDisc('round shadows', { radius: 1, tessellation: 24 }, scene);
    this.disc.rotation.x = Math.PI / 2; // flat, facing up
    this.disc.bakeCurrentTransformIntoVertices();
    this.disc.material = palette.disc();
    this.disc.isPickable = false;
    this.disc.alwaysSelectAsActiveMesh = true; // the instances lie wherever the bodies are
    this.disc.alphaIndex = SHADOW_ORDER;
    this.disc.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
    this.disc.thinInstanceCount = 0;
  }

  /** Starts a frame's discs round the camera's target at (x, z). */
  begin(x: number, z: number): void {
    this.cx = x;
    this.cz = z;
    this.count = 0;
  }

  /** A disc for a body of radius r with its feet at (x, y, z), if it's within SHADOW_M of the target: on the ground under its feet, a solid's top it stands on too, and on the terrain sheared to the terrain's slope across it, so no edge buries itself uphill. */
  add(x: number, y: number, z: number, r: number): void {
    if (Math.hypot(x - this.cx, z - this.cz) > SHADOW_M) {
      return;
    }
    if (16 * (this.count + 1) > this.matrices.length) {
      const grown = new Float32Array(2 * this.matrices.length);
      grown.set(this.matrices);
      this.matrices = grown;
      this.disc.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
    }
    const s = r * SPREAD;
    const t = this.map.geometry.terrain;
    const ground = groundUnder(this.map, x, z, y);
    const terrain = Math.abs(ground - height(t, x, z)) < ON_TERRAIN_M;
    const slopeX = terrain ? (height(t, x + s, z) - height(t, x - s, z)) / (2 * s) : 0;
    const slopeZ = terrain ? (height(t, x, z + s) - height(t, x, z - s)) / (2 * s) : 0;
    const m = this.matrices;
    const o = 16 * this.count++;
    m.fill(0, o, o + 16);
    m[o] = s;
    m[o + 1] = s * slopeX; // a step along x rises as the ground does
    m[o + 5] = 1;
    m[o + 9] = s * slopeZ; // and along z
    m[o + 10] = s;
    m[o + 12] = x;
    m[o + 13] = ground + LIFT_M;
    m[o + 14] = z;
    m[o + 15] = 1;
  }

  /** Draws the discs added since begin. */
  end(): void {
    this.disc.thinInstanceCount = this.count;
    this.disc.thinInstanceBufferUpdated('matrix');
  }
}
