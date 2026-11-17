// The lock's ring (target-lock.md section 5): a gold ring on the ground under the locked mob, turning so it reads on any ground, drawn after the mobs' red areas; a decal, opening with the place, and shown faintly where the place hides it.
import { VertexBuffer } from '@babylonjs/core/Buffers/buffer';
import { Constants } from '@babylonjs/core/Engines/constants';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData';
import type { Scene } from '@babylonjs/core/scene';

import { groundUnder } from './map';
import type { WorldMap } from './map';
import { behindPlace, GHOST, ghostOf } from './see-through';
import type { Palette } from './toon';

const COLOR = new Color3(1, 0.86, 0.45); // the outline's gold
const PAD_M = 0.15; // the ring's middle past the mob's radius
const WIDTH_M = 0.08;
const LIFT_M = 0.05; // just over the ground, above the telegraphs' 0.04
const TURN_MS = 4000; // one full turn
const DASHES = 8; // the gaps between them show it turning
const FILL = 0.7; // of each dash's share of the turn
const STEPS = 6; // segments a dash
const GROUP = 1; // a rendering group drawn after the scene's own, on its depth, so a red area under the mob never covers it

export class LockRing {
  private readonly mesh: Mesh;
  private readonly positions: Float32Array;

  constructor(
    scene: Scene,
    private readonly map: WorldMap,
    palette: Palette,
  ) {
    const m = new Mesh('lock ring', scene);
    const data = new VertexData();
    this.positions = new Float32Array(DASHES * (STEPS + 1) * 2 * 3);
    data.positions = this.positions;
    const indices: number[] = [];
    for (let d = 0; d < DASHES; d++) {
      for (let s = 0; s < STEPS; s++) {
        const a = (d * (STEPS + 1) + s) * 2;
        indices.push(a, a + 1, a + 2, a + 1, a + 3, a + 2);
      }
    }
    data.indices = indices;
    data.applyToMesh(m, true);
    m.material = palette.decal(COLOR, 1, Constants.ALPHA_DISABLE);
    m.isPickable = false;
    m.renderingGroupId = GROUP;
    scene.setRenderingAutoClearDepthStencil(GROUP, false, false, false);
    const ghost = new StandardMaterial('lock ring ghost', scene);
    ghost.disableLighting = true;
    ghost.emissiveColor = COLOR;
    ghost.backFaceCulling = false;
    ghost.fogEnabled = false;
    ghost.alpha = GHOST;
    ghost.zOffset = -2;
    behindPlace(ghost);
    ghostOf(m, ghost);
    m.setEnabled(false);
    this.mesh = m;
  }

  /** Lays it under a mob of radius at (x, z), feet at y, turned for now; or hides it with none. */
  show(
    mob: {
      readonly x: number;
      readonly y: number;
      readonly z: number;
      readonly radius: number;
    } | null,
    now: number,
  ): void {
    const m = this.mesh;
    if (!mob) {
      m.setEnabled(false);
      return;
    }
    const ground = groundUnder(this.map, mob.x, mob.z, mob.y);
    m.position.set(mob.x, ground, mob.z);
    const turn = ((now % TURN_MS) / TURN_MS) * 2 * Math.PI;
    const r = mob.radius + PAD_M;
    const share = (2 * Math.PI) / DASHES;
    const p = this.positions;
    for (let d = 0; d < DASHES; d++) {
      for (let s = 0; s <= STEPS; s++) {
        const a = turn + d * share + (s / STEPS) * share * FILL;
        const [sin, cos] = [Math.sin(a), Math.cos(a)];
        const i = (d * (STEPS + 1) + s) * 6;
        for (const [k, at] of [r + WIDTH_M / 2, r - WIDTH_M / 2].entries()) {
          const x = sin * at;
          const z = cos * at;
          p.set(
            [x, groundUnder(this.map, mob.x + x, mob.z + z, mob.y) - ground + LIFT_M, z],
            i + k * 3,
          );
        }
      }
    }
    m.updateVerticesData(VertexBuffer.PositionKind, p);
    m.refreshBoundingInfo();
    m.setEnabled(true);
  }

  dispose(): void {
    for (const m of [this.mesh, ...this.mesh.getChildMeshes()]) {
      m.material?.dispose();
    }
    this.mesh.dispose();
  }
}
