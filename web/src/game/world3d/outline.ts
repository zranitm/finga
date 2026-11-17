// The lock's outline (target-lock.md section 5) and hard immunity's (statuses.md section 6): Babylon's HighlightLayer round the locked mob in gold, and round each body hard immunity guards in a faint white.
import { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import '@babylonjs/core/Layers/effectLayerSceneComponent';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Figure } from './body';

const COLOR = new Color3(1, 0.86, 0.45);
const IMMUNE = new Color3(0.5, 0.52, 0.56); // a faint white

/** The meshes of figure the layer outlines: its drawn ones, not its ink. */
function meshesOf(figure: Figure): Mesh[] {
  return figure.root
    .getChildMeshes(false)
    .filter(
      (m): m is Mesh => 'subMeshes' in m && !m.name.endsWith(' ink') && m.getTotalVertices() > 0,
    );
}

export class Outline {
  private readonly layer: HighlightLayer;
  private marked: { readonly figure: Figure; readonly meshes: readonly Mesh[] } | null = null;
  private readonly immune = new Map<Figure, readonly Mesh[]>();

  constructor(scene: Scene) {
    this.layer = new HighlightLayer('aim', scene, {
      mainTextureRatio: 0.5,
      blurHorizontalSize: 0.6,
      blurVerticalSize: 0.6,
    });
    this.layer.innerGlow = false;
  }

  /** Outlines figure alone in gold, or none; one hard immunity guards keeps its white. */
  mark(figure: Figure | null): void {
    if (figure === (this.marked?.figure ?? null)) {
      return;
    }
    if (this.marked) {
      this.unmark(this.marked.meshes, this.immune.has(this.marked.figure));
    }
    const meshes = figure ? meshesOf(figure) : [];
    for (const m of meshes) {
      this.layer.addMesh(m, COLOR);
    }
    this.marked = figure ? { figure, meshes } : null;
  }

  /** Outlines in a faint white the figures hard immunity guards now, the locked one keeping its gold; the rest lose theirs. */
  guarded(figures: ReadonlySet<Figure>): void {
    for (const [figure, meshes] of this.immune) {
      if (!figures.has(figure)) {
        this.immune.delete(figure);
        if (figure !== this.marked?.figure) {
          this.unmark(meshes, false);
        }
      }
    }
    for (const figure of figures) {
      if (!this.immune.has(figure)) {
        const meshes = figure === this.marked?.figure ? this.marked.meshes : meshesOf(figure);
        this.immune.set(figure, meshes);
        if (figure !== this.marked?.figure) {
          for (const m of meshes) {
            this.layer.addMesh(m, IMMUNE);
          }
        }
      }
    }
  }

  dispose(): void {
    this.marked = null;
    this.immune.clear();
    this.layer.dispose();
  }

  /** Takes a figure's outline away, or back to white while hard immunity still guards it. */
  private unmark(meshes: readonly Mesh[], guarded: boolean): void {
    for (const m of meshes) {
      if (guarded) {
        this.layer.addMesh(m, IMMUNE);
      } else {
        this.layer.removeMesh(m);
      }
    }
  }
}
