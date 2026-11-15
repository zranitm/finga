// Names, NPC markers and portal labels (3d-world.md section 10): DOM text over the canvas, each at its world point projected to the screen every frame.
import type { Camera } from '@babylonjs/core/Cameras/camera';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { Scene } from '@babylonjs/core/scene';

export type LabelKind = 'self' | 'player' | 'npc' | 'marker' | 'portal';

const STYLE: Readonly<Record<LabelKind, string>> = {
  self: 'color:#fff7c2;font:600 13px sans-serif',
  player: 'color:#f4f4f8;font:600 13px sans-serif',
  npc: 'color:#ffe08a;font:600 13px sans-serif',
  marker: 'color:#ffd23f;font:800 22px sans-serif',
  portal: 'color:#cfe8ff;font:600 12px sans-serif',
};

/** The marker over an NPC with something for the player: a quest to hand in, to take, or under way. */
export const MARKER_TEXT = { ready: '?', available: '!', active: '…' } as const;

/** A label at a world point, its text and the pixels it rises over the point. */
interface Label {
  readonly el: HTMLDivElement;
  readonly at: Vector3;
  lift: number;
}

export class Labels {
  private readonly layer: HTMLDivElement;
  private readonly labels = new Map<string, Label>();
  private readonly projected = new Vector3();

  constructor(parent: HTMLElement) {
    this.layer = document.createElement('div');
    this.layer.style.cssText = 'position:absolute;inset:0;pointer-events:none;overflow:hidden';
    parent.appendChild(this.layer);
  }

  /** Shows text for key at a world point lift pixels up, or changes it; an empty text hides it. */
  set(key: string, kind: LabelKind, text: string, x: number, y: number, z: number, lift = 0): void {
    let l = this.labels.get(key);
    if (!l) {
      const el = document.createElement('div');
      el.style.cssText = `position:absolute;left:0;top:0;white-space:nowrap;text-shadow:0 0 3px #0a0c16,0 0 2px #0a0c16;${STYLE[kind]}`;
      el.dataset.label = key;
      this.layer.appendChild(el);
      l = { el, at: new Vector3(), lift };
      this.labels.set(key, l);
    }
    if (l.el.textContent !== text) {
      l.el.textContent = text;
    }
    l.at.set(x, y, z);
    l.lift = lift;
  }

  delete(key: string): void {
    this.labels.get(key)?.el.remove();
    this.labels.delete(key);
  }

  /** Drops every label whose key starts with prefix. */
  clear(prefix = ''): void {
    for (const key of [...this.labels.keys()]) {
      if (key.startsWith(prefix)) {
        this.delete(key);
      }
    }
  }

  /** Places every label at its point as camera sees it; one behind the camera or off the screen hides. */
  place(scene: Scene, camera: Camera): void {
    const engine = scene.getEngine();
    const w = engine.getRenderWidth();
    const h = engine.getRenderHeight();
    const viewport = camera.viewport.toGlobal(w, h);
    const transform = scene.getTransformMatrix();
    const scale = this.layer.clientWidth > 0 && w > 0 ? this.layer.clientWidth / w : 1;
    for (const l of this.labels.values()) {
      Vector3.ProjectToRef(l.at, Matrix.IdentityReadOnly, transform, viewport, this.projected);
      const p = this.projected;
      const shown =
        l.el.textContent !== '' &&
        p.z >= 0 &&
        p.z <= 1 &&
        p.x >= 0 &&
        p.x <= w &&
        p.y >= 0 &&
        p.y <= h;
      l.el.style.display = shown ? '' : 'none';
      if (shown) {
        l.el.style.transform = `translate(-50%,-100%) translate(${p.x * scale}px,${p.y * scale - l.lift}px)`;
      }
    }
  }

  dispose(): void {
    this.layer.remove();
    this.labels.clear();
  }
}
