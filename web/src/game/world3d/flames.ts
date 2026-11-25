// A cave's torch flames (underground-and-underwater.md section 7): each drawn unlit at full colour with a warm glow card behind it, both thin instances, and each scaled by its group's flicker as its light is.
import { CreatePlane } from '@babylonjs/core/Meshes/Builders/planeBuilder';
import '@babylonjs/core/Meshes/thinInstanceMesh'; // the thin instance methods on Mesh
import { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import { TORCH_SCALE } from './kit-models';
import type { Models } from './models';
import { baseOf } from './place';
import type { Flame } from './region-bake';
import type { Palette } from './toon';
import { flicker } from './torchlight';

const FLAME_FILE = 'kaykit/torch_flame.glb'; // the flame round its cup's centre, as a torch's light stands
const CARD_M = 0.9; // a glow card's size at full flicker
const CARD_LIFT_M = 0.08; // its middle over the cup's centre, at the flame's
const CARD_BACK_M = 0.12; // and behind it from the camera, so the flame draws over it

/** The flames of a cave's torches, each at its light's spot. */
export class Flames {
  private flame: Mesh | null = null;
  private readonly card: Mesh;
  private readonly flames: Float32Array;
  private readonly cards: Float32Array;

  constructor(
    scene: Scene,
    private readonly palette: Palette,
    models: Models,
    private readonly lights: readonly Flame[],
  ) {
    this.flames = new Float32Array(16 * lights.length);
    this.cards = new Float32Array(16 * lights.length);
    this.card = CreatePlane('glow cards', { size: 1 }, scene);
    this.card.material = palette.flameGlow();
    this.ready(this.card, this.cards);
    models
      .load(FLAME_FILE)
      .then((container) => {
        if (this.card.isDisposed()) {
          return; // its scene went while it loaded
        }
        const made = container.instantiateModelsToScene((n) => `flame ${n}`, false, {
          doNotInstantiate: true,
        });
        const mesh = made.rootNodes
          .flatMap((r) => r.getChildMeshes(false))
          .find((m): m is Mesh => m instanceof Mesh && m.material !== null);
        if (!mesh) {
          throw new Error(`${FLAME_FILE} holds no mesh`);
        }
        const world = mesh.computeWorldMatrix(true).clone();
        mesh.parent = null;
        mesh.bakeTransformIntoVertices(world);
        mesh.position.setAll(0);
        mesh.rotationQuaternion = null;
        mesh.rotation.setAll(0);
        mesh.scaling.setAll(1);
        mesh.material = this.palette.unlit(baseOf(mesh.material!));
        for (const r of made.rootNodes) {
          r.dispose(false, false);
        }
        this.ready(mesh, this.flames);
        this.flame = mesh;
      })
      .catch((err: unknown) => {
        console.info(`finga: ${FLAME_FILE} didn't load: ${String(err)}`); // the torches still light the cave
      });
  }

  /** Makes m draw buffer's instances, one a light, wherever they stand. */
  private ready(m: Mesh, buffer: Float32Array): void {
    m.isPickable = false;
    m.alwaysSelectAsActiveMesh = true;
    m.thinInstanceSetBuffer('matrix', buffer, 16, false);
  }

  /** Draws each flame at seconds since the engine started, its group's flicker its size, its glow card turned to the camera at (eyeX, eyeZ) and set behind it. */
  frame(seconds: number, eyeX: number, eyeZ: number): void {
    const swing0 = flicker(0, seconds);
    const swing1 = flicker(1, seconds);
    const f = this.flames;
    const c = this.cards;
    for (let i = 0; i < this.lights.length; i++) {
      const l = this.lights[i]!;
      const s = l.group === 0 ? swing0 : swing1;
      const o = 16 * i;
      f.fill(0, o, o + 16);
      f[o] = TORCH_SCALE * s;
      f[o + 5] = TORCH_SCALE * s;
      f[o + 10] = TORCH_SCALE * s;
      f[o + 12] = l.x;
      f[o + 13] = l.y;
      f[o + 14] = l.z;
      f[o + 15] = 1;
      const dx = eyeX - l.x;
      const dz = eyeZ - l.z;
      const d = Math.sqrt(dx * dx + dz * dz) || 1;
      const k = CARD_M * s;
      c.fill(0, o, o + 16);
      c[o] = (k * dz) / d; // its face square to the camera's line
      c[o + 2] = (-k * dx) / d;
      c[o + 5] = k;
      c[o + 8] = (k * dx) / d;
      c[o + 10] = (k * dz) / d;
      c[o + 12] = l.x - (CARD_BACK_M * dx) / d;
      c[o + 13] = l.y + CARD_LIFT_M;
      c[o + 14] = l.z - (CARD_BACK_M * dz) / d;
      c[o + 15] = 1;
    }
    this.card.thinInstanceBufferUpdated('matrix');
    this.flame?.thinInstanceBufferUpdated('matrix');
  }
}
