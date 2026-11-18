// A shield's bubble (skill-roster.md section 7): a pale sphere round each player with shield left, brightest at its rim so the body shows through it, breathing slowly.
import { FresnelParameters } from '@babylonjs/core/Materials/fresnelParameters';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { CreateSphere } from '@babylonjs/core/Meshes/Builders/sphereBuilder';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { movement } from '@/gamedata/movement';

import { SHIELD } from '../effects/colors';
import type { Point } from '../effects/types';

const PAD_M = 0.35; // past the body's sides
const OVER_M = 0.3; // over its head
const BREATH_MS = 1600;
const BREATH = 0.03; // of its size, either way
const BASE_ALPHA = 0.01; // the fresnel adds the rest: faint on its face, near solid at its rim

export class ShieldBubbles {
  private readonly material: StandardMaterial;
  private readonly shown = new Map<string, Mesh>();
  private readonly spare: Mesh[] = [];

  constructor(private readonly scene: Scene) {
    const m = new StandardMaterial('shield bubble', scene);
    m.disableLighting = true;
    m.diffuseColor = Color3.Black();
    m.specularColor = Color3.Black();
    m.emissiveColor = Color3.FromInts((SHIELD >> 16) & 0xff, (SHIELD >> 8) & 0xff, SHIELD & 0xff);
    m.alpha = BASE_ALPHA;
    m.opacityFresnelParameters = new FresnelParameters({
      leftColor: new Color3(0.8, 0.8, 0.8), // its rim
      rightColor: new Color3(0.1, 0.1, 0.1), // its face, which the body shows through
      power: 2,
      bias: 0,
    });
    this.material = m;
    const first = this.make();
    first.setEnabled(false);
    this.spare.push(first);
    first.scaling.set(1, 2, 1); // stretched, as a bubble always shows, which takes its own shader
    first.computeWorldMatrix(true);
    m.forceCompilation(first); // so the first shield doesn't stall a frame
  }

  /** Each player with shield left, by key, standing at their point now; the others' bubbles go. */
  show(on: ReadonlyMap<string, Point>, now: number): void {
    for (const [key, mesh] of this.shown) {
      if (!on.has(key)) {
        mesh.setEnabled(false);
        this.spare.push(mesh);
        this.shown.delete(key);
      }
    }
    const k = 1 + BREATH * Math.sin((2 * Math.PI * now) / BREATH_MS);
    const w = 2 * (movement.radius + PAD_M) * k;
    const h = (movement.height + OVER_M) * k;
    for (const [key, at] of on) {
      let mesh = this.shown.get(key);
      if (!mesh) {
        mesh = this.spare.pop() ?? this.make();
        mesh.setEnabled(true);
        this.shown.set(key, mesh);
      }
      mesh.scaling.set(w, h, w);
      mesh.position.set(at.x, at.y + h / 2 - OVER_M / 2, at.z);
    }
  }

  dispose(): void {
    for (const mesh of [...this.shown.values(), ...this.spare]) {
      mesh.dispose();
    }
    this.shown.clear();
    this.spare.length = 0;
    this.material.dispose();
  }

  private make(): Mesh {
    const mesh = CreateSphere('shield bubble', { diameter: 1, segments: 16 }, this.scene);
    mesh.material = this.material;
    mesh.isPickable = false;
    return mesh;
  }
}
