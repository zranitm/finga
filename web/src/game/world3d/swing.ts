// A swing's trail (3d-world.md section 10): a TrailMesh ribbon the length of the weapon, shown through the strike beat.
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import { TrailMesh } from '@babylonjs/core/Meshes/trailMesh';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode';
import type { Scene } from '@babylonjs/core/scene';

import type { Beats } from './beats';

/** Frames of the weapon's path the ribbon keeps, and its opacity at the hit, fading out to the strike's end. */
const TRAIL_FRAMES = 12;
const PEAK = 0.85;

export class Swing {
  private readonly trail: TrailMesh;
  private readonly material: StandardMaterial;
  private beats: Beats | null = null;

  constructor(scene: Scene, weapon: AbstractMesh) {
    const { minimum, maximum } = weapon.getBoundingInfo().boundingBox;
    const size = maximum.subtract(minimum);
    const along = size.x >= size.y && size.x >= size.z ? 'x' : size.y >= size.z ? 'y' : 'z';
    // TrailMesh draws its section along the generator's x: the blade lies along it.
    const blade = new TransformNode('blade', scene);
    blade.parent = weapon;
    blade.position = minimum.add(maximum).scale(0.5);
    if (along === 'y') {
      blade.rotation.z = Math.PI / 2;
    } else if (along === 'z') {
      blade.rotation.y = Math.PI / 2;
    }
    this.trail = new TrailMesh('swing', blade, scene, {
      diameter: size[along] / 2,
      length: TRAIL_FRAMES,
      sections: 2,
    });
    this.material = new StandardMaterial('swing', scene);
    this.material.emissiveColor = Color3.White();
    this.material.disableLighting = true;
    this.material.backFaceCulling = false;
    this.trail.material = this.material;
    this.trail.isVisible = false;
  }

  /** The action whose strike the ribbon shows. */
  follow(beats: Beats): void {
    this.beats = beats;
  }

  update(now: number): void {
    const b = this.beats;
    const on = b !== null && now >= b.hit && now < b.strikeEnd;
    this.trail.isVisible = on;
    if (on) {
      this.material.alpha = PEAK * (1 - (now - b.hit) / (b.strikeEnd - b.hit));
    }
  }

  dispose(): void {
    this.trail.dispose();
    this.material.dispose();
  }
}
