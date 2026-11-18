// A match map's prep wall (pvp.md section 3): a see-through sheet of the portals' blue across the middle, up through prep and the count into a round, drawn from the wall's boxes.
import { Constants } from '@babylonjs/core/Engines/constants';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Box } from '@/sim/step';
import { height } from '@/sim/terrain';
import type { Terrain } from '@/sim/terrain';

const LIGHT = Color3.FromHexString('#5fb8ff'); // added light saturated to white on sand
const ALPHA = 0.4;
const SINK = 0.5; // as a solid's, so a slope never shows its foot

export class PrepWall {
  private readonly meshes: Mesh[];
  private readonly material: StandardMaterial;

  constructor(scene: Scene, boxes: readonly Box[], t: Terrain) {
    const m = new StandardMaterial('prep wall', scene);
    m.disableLighting = true;
    m.emissiveColor = LIGHT;
    m.alpha = ALPHA;
    m.alphaMode = Constants.ALPHA_COMBINE;
    m.backFaceCulling = false;
    this.material = m;
    this.meshes = boxes.map((b) => {
      const bottom = height(t, b.x, b.z) - SINK;
      const mesh = CreateBox(
        'prep wall',
        { width: 2 * b.half_w, depth: 2 * b.half_d, height: b.top - bottom },
        scene,
      );
      mesh.position.set(b.x, (b.top + bottom) / 2, b.z);
      mesh.rotation.y = Math.atan2(-b.uz, b.ux); // its width runs along (ux, uz)
      mesh.material = m;
      mesh.isPickable = false;
      mesh.setEnabled(false);
      return mesh;
    });
  }

  show(up: boolean): void {
    for (const m of this.meshes) {
      m.setEnabled(up);
    }
  }

  dispose(): void {
    for (const m of this.meshes) {
      m.dispose();
    }
    this.material.dispose();
  }
}
