// The lock's outline (target-lock.md section 5): Babylon's HighlightLayer round the locked mob.
import type { Camera } from '@babylonjs/core/Cameras/camera';
import { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer';
import '@babylonjs/core/Layers/effectLayerSceneComponent';
import { Color3 } from '@babylonjs/core/Maths/math.color';
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { AbstractMesh } from '@babylonjs/core/Meshes/abstractMesh';
import type { Mesh } from '@babylonjs/core/Meshes/mesh';
import type { SubMesh } from '@babylonjs/core/Meshes/subMesh';
import type { Scene } from '@babylonjs/core/scene';

import type { Figure } from './body';

const COLOR = new Color3(1, 0.86, 0.45);

/** A locked figure, and the cylinder it's drawn in: its height and radius. */
export interface Locked {
  readonly figure: Figure;
  readonly height: number;
  readonly radius: number;
}
const REACH_M = 0.5; // past the locked figure's bounds, more than its glow reaches on screen at any zoom

export class Outline {
  private readonly layer: HighlightLayer;
  private marked: { readonly lock: Locked; readonly meshes: readonly Mesh[] } | null = null;
  private readonly seen = new WeakSet<AbstractMesh>();
  private building: SubMesh[] = [];
  private readonly eye = new Vector3();
  private readonly to = new Vector3(); // from the eye to the locked figure's middle
  private reach = Infinity; // the figure's and its glow's reach round its middle: till a cull, none is left out
  private widen = 1; // a cone's radius over its axis's, at the eye's distance from the figure
  private readonly off = new Vector3();
  private readonly closest = new Vector3();

  constructor(private readonly scene: Scene) {
    this.layer = new HighlightLayer('aim', scene, {
      mainTextureRatio: 0.5,
      blurHorizontalSize: 0.6,
      blurVerticalSize: 0.6,
    });
    this.layer.innerGlow = false;
    // Babylon asks the layer's inner pass this of every mesh it would draw; nothing public filters it as cheaply.
    const pass = (
      this.layer as unknown as { _thinEffectLayer: { _shouldRenderMesh(m: AbstractMesh): boolean } }
    )._thinEffectLayer;
    const own = pass._shouldRenderMesh.bind(pass);
    pass._shouldRenderMesh = (m) => own(m) && this.covers(m);
  }

  /** Outlines the locked figure alone, or none. */
  mark(lock: Locked | null): void {
    const figure = lock?.figure ?? null;
    if (figure === (this.marked?.lock.figure ?? null)) {
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
    this.marked = lock ? { lock, meshes } : null;
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

  /**
   * Each frame, before the render: limits the layer's pass to the locked figure and the meshes that could cover part of it.
   * Only a mesh in the cone from the eye round the figure can; the rest can't touch its outline.
   */
  cull(camera: Camera): void {
    if (!this.marked) {
      return; // the layer draws nothing without a lock
    }
    const { figure, height, radius } = this.marked.lock; // a skinned mesh's bounds don't follow its bones
    this.reach = Math.hypot(height / 2, radius) + REACH_M;
    camera.getViewMatrix(true); // where this frame's move put it
    this.eye.copyFrom(camera.globalPosition);
    figure.root.getAbsolutePosition().subtractToRef(this.eye, this.to);
    this.to.y += height / 2;
    const away = this.to.length();
    this.widen = away / Math.sqrt(Math.max(away * away - this.reach * this.reach, 1e-6));
  }

  /** Whether m could cover part of the locked figure: its bounds within the cone from the eye round the figure, which past the figure goes on as wide. */
  private covers(m: AbstractMesh): boolean {
    if (this.reach === Infinity) {
      return true;
    }
    const sphere = m.getBoundingInfo().boundingSphere;
    sphere.centerWorld.subtractToRef(this.eye, this.off);
    const t = Math.min(1, Math.max(0, Vector3.Dot(this.off, this.to) / this.to.lengthSquared()));
    this.to.scaleToRef(t, this.closest);
    return (
      Vector3.Distance(this.off, this.closest) <= (t * this.reach + sphere.radiusWorld) * this.widen
    );
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
