// A cave's round shadows (underground-and-underwater.md section 7): with no sun, each body near the camera's target has a soft dark disc on the ground under it, all of them thin instances of one disc.
import { CreateDisc } from '@babylonjs/core/Meshes/Builders/discBuilder';
import '@babylonjs/core/Meshes/thinInstanceMesh'; // the thin instance methods on Mesh
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Palette } from './toon';

export const SHADOW_M = 25; // a body this far from the camera's target or nearer has one, as others cast the sun's within it on the land
const SPREAD = 1.6; // a disc's radius over its body's
const LIFT_M = 0.04; // over the ground, so it never fights it

/** Each frame's discs: begun round the camera's target, one added for each body, then drawn. */
export class RoundShadows {
  private readonly disc: Mesh;
  private matrices = new Float32Array(16 * 32);
  private count = 0;
  private cx = 0;
  private cz = 0;

  constructor(scene: Scene, palette: Palette) {
    this.disc = CreateDisc('round shadows', { radius: 1, tessellation: 24 }, scene);
    this.disc.rotation.x = Math.PI / 2; // flat, facing up
    this.disc.bakeCurrentTransformIntoVertices();
    this.disc.material = palette.disc();
    this.disc.isPickable = false;
    this.disc.alwaysSelectAsActiveMesh = true; // the instances lie wherever the bodies are
    this.disc.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
    this.disc.thinInstanceCount = 0;
  }

  /** Starts a frame's discs round the camera's target at (x, z). */
  begin(x: number, z: number): void {
    this.cx = x;
    this.cz = z;
    this.count = 0;
  }

  /** A disc for a body of radius r over the ground at (x, ground, z), if it's within SHADOW_M of the target. */
  add(x: number, ground: number, z: number, r: number): void {
    if (Math.hypot(x - this.cx, z - this.cz) > SHADOW_M) {
      return;
    }
    if (16 * (this.count + 1) > this.matrices.length) {
      const grown = new Float32Array(2 * this.matrices.length);
      grown.set(this.matrices);
      this.matrices = grown;
      this.disc.thinInstanceSetBuffer('matrix', this.matrices, 16, false);
    }
    const m = this.matrices;
    const o = 16 * this.count++;
    m.fill(0, o, o + 16);
    m[o] = r * SPREAD;
    m[o + 5] = 1;
    m[o + 10] = r * SPREAD;
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

  dispose(): void {
    this.disc.dispose();
  }
}
