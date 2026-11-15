// Names, NPC markers and portal labels (3d-world.md section 10): DOM text over the canvas, each at its world point projected to the screen every frame.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';

import type { Overlay, ScreenPoint } from './overlay';

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
  private readonly labels = new Map<string, Label>();
  private readonly screen: ScreenPoint = { x: 0, y: 0 };

  constructor(private readonly overlay: Overlay) {}

  /** Shows text for key at a world point lift pixels up, or changes it; an empty text hides it. */
  set(key: string, kind: LabelKind, text: string, x: number, y: number, z: number, lift = 0): void {
    let l = this.labels.get(key);
    if (!l) {
      const el = document.createElement('div');
      el.style.cssText = `position:absolute;left:0;top:0;white-space:nowrap;text-shadow:0 0 3px #0a0c16,0 0 2px #0a0c16;${STYLE[kind]}`;
      el.dataset.label = key;
      this.overlay.layer.appendChild(el);
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

  clear(): void {
    for (const l of this.labels.values()) {
      l.el.remove();
    }
    this.labels.clear();
  }

  /** Places every label at its point as the overlay's frame sees it; one behind the camera or off the screen hides. */
  place(): void {
    const p = this.screen;
    for (const l of this.labels.values()) {
      const shown = l.el.textContent !== '' && this.overlay.project(l.at.x, l.at.y, l.at.z, p);
      l.el.style.display = shown ? '' : 'none';
      if (shown) {
        l.el.style.transform = `translate(-50%,-100%) translate(${p.x}px,${p.y - l.lift}px)`;
      }
    }
  }

  dispose(): void {
    this.clear();
  }
}
