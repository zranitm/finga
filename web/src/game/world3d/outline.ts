// The auto-aim's outline (3d-world.md section 6): Babylon's HighlightLayer round the mob a basic attack would pick now.
import { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import '@babylonjs/core/Layers/effectLayerSceneComponent';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Figure } from './body';

const COLOR = new Color3(1, 0.86, 0.45);

export class Outline {
  private readonly layer: HighlightLayer;
  private marked: { readonly figure: Figure; readonly meshes: readonly Mesh[] } | null = null;

  constructor(scene: Scene) {
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

  dispose(): void {
    this.marked = null;
    this.layer.dispose();
  }
}
