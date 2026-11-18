// The lock's outline (target-lock.md section 5): Babylon's HighlightLayer round the locked mob.
import { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import '@babylonjs/core/Layers/effectLayerSceneComponent';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { SubMesh } from '@babylonjs/core/Meshes/subMesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Figure } from './body';

const COLOR = new Color3(1, 0.86, 0.45);

export class Outline {
  private readonly layer: HighlightLayer;
  private marked: { readonly figure: Figure; readonly meshes: readonly Mesh[] } | null = null;
  private readonly seen = new WeakSet<AbstractMesh>();
  private building: SubMesh[] = [];

  constructor(private readonly scene: Scene) {
    this.layer = new HighlightLayer('aim', scene, {
      mainTextureRatio: 0.5,
      blurHorizontalSize: 0.6,
      blurVerticalSize: 0.6,
    });
    this.layer.innerGlow = false;
  }

  /** Outlines figure alone, or none. */
  mark(figure: Figure | null): void {
    if (figure === (this.marked?.figure ?? null)) {
      return;
    }
    for (const m of this.marked?.meshes ?? []) {
      this.layer.removeMesh(m);
    }
    const meshes = figure
      ? figure.root
          .getChildMeshes(false)
          .filter(
            (m): m is Mesh =>
              'subMeshes' in m && !m.name.endsWith(' ink') && m.getTotalVertices() > 0,
          )
      : [];
    for (const m of meshes) {
      this.layer.addMesh(m, COLOR);
    }
    this.marked = figure ? { figure, meshes } : null;
  }

  /** Each frame: builds the outline's shaders for each mesh new to the scene, as its sources load, since the outline draws every mesh in view, the locked one bright and the rest dark; so the first lock doesn't stall a frame. */
  ready(): void {
    for (const m of this.scene.meshes) {
      if (!this.seen.has(m) && m.subMeshes?.length) {
        this.seen.add(m);
        this.building.push(...m.subMeshes);
      }
    }
    this.building = this.building.filter((s) => !this.built(s));
  }

  dispose(): void {
    this.marked = null;
    this.layer.dispose();
  }

  /** Whether the outline's shader for s is built, or never wanted: a gone mesh's, or one with no material. */
  private built(s: SubMesh): boolean {
    const m = s.getRenderingMesh();
    return (
      m.isDisposed() ||
      !s.getMaterial() ||
      this.layer.isReady(s, m.hasThinInstances || m.instances.length > 0)
    );
  }
}
