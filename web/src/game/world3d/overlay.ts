// The DOM layer over the canvas (3d-world.md section 10) that names, markers and damage numbers sit in, and where a world point falls on it this frame.
import type { Camera } from '@babylonjs/core/Cameras/camera';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Viewport } from '@babylonjs/core/Maths/math.viewport';
import type { Scene } from '@babylonjs/core/scene';

/** A point on the layer, in CSS pixels from its top left. */
export interface ScreenPoint {
  x: number;
  y: number;
}

export class Overlay {
  readonly layer: HTMLDivElement;
  private readonly resize: ResizeObserver;
  private width = 0; // the layer's CSS width, kept as it changes so a frame needn't ask for layout
  private readonly projected = new Vector3();
  private readonly at = new Vector3();
  private view: {
    readonly w: number;
    readonly h: number;
    readonly scale: number;
    readonly viewport: Viewport;
    readonly transform: Matrix;
  } | null = null;

  constructor(parent: HTMLElement) {
    this.layer = document.createElement('div');
    this.layer.style.cssText = 'position:absolute;inset:0;pointer-events:none;overflow:hidden';
    parent.appendChild(this.layer);
    this.width = this.layer.clientWidth;
    this.resize = new ResizeObserver(() => (this.width = this.layer.clientWidth));
    this.resize.observe(this.layer);
  }

  /** Takes this frame's view: every point projected until the next is seen from camera. */
  frame(scene: Scene, camera: Camera): void {
    const engine = scene.getEngine();
    const w = engine.getRenderWidth();
    const h = engine.getRenderHeight();
    this.view = {
      w,
      h,
      scale: this.width > 0 && w > 0 ? this.width / w : 1,
      viewport: camera.viewport.toGlobal(w, h),
      transform: scene.getTransformMatrix(),
    };
  }

  /** Where (x, y, z) falls on the layer, into out; false when it's behind the camera or off the screen, or no frame was seen yet. */
  project(x: number, y: number, z: number, out: ScreenPoint): boolean {
    const v = this.view;
    if (!v) {
      return false;
    }
    Vector3.ProjectToRef(
      this.at.set(x, y, z),
      Matrix.IdentityReadOnly,
      v.transform,
      v.viewport,
      this.projected,
    );
    const p = this.projected;
    if (p.z < 0 || p.z > 1 || p.x < 0 || p.x > v.w || p.y < 0 || p.y > v.h) {
      return false;
    }
    out.x = p.x * v.scale;
    out.y = p.y * v.scale;
    return true;
  }

  /** Forgets the view, as a new map's camera replaces it. */
  reset(): void {
    this.view = null;
  }

  dispose(): void {
    this.resize.disconnect();
    this.layer.remove();
  }
}
