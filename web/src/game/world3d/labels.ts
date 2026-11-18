// Names, NPC markers, portal labels and the talk key's prompt (3d-world.md section 10, key-bindings.md section 6): DOM text over the canvas, each at its world point projected to the screen every frame, players' names lifted clear of the rest.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';

import { stackTags } from '@/lib/tags';
import type { TagBox } from '@/lib/tags';

import type { Overlay, ScreenPoint } from './overlay';

export type LabelKind = 'self' | 'player' | 'npc' | 'marker' | 'portal' | 'prompt';

const STYLE: Readonly<Record<LabelKind, string>> = {
  self: 'color:#fff7c2;font:600 13px sans-serif',
  player: 'color:#f4f4f8;font:600 13px sans-serif',
  npc: 'color:#ffe08a;font:600 13px sans-serif',
  marker: 'color:#ffd23f;font:800 22px sans-serif',
  portal: 'color:#cfe8ff;font:600 12px sans-serif',
  prompt:
    'color:#f4f4f8;font:600 12px sans-serif;background:rgba(10,12,22,0.72);border:1px solid rgba(207,232,255,0.5);border-radius:4px;padding:1px 6px',
};

/** A label at a world point: its text, the pixels it rises over the point, and its size once laid out. */
interface Label {
  readonly el: HTMLDivElement;
  readonly kind: LabelKind;
  readonly at: Vector3;
  lift: number;
  w: number;
  h: number;
}

/** A label shown this frame and where it sits. */
interface Placed {
  readonly l: Label;
  readonly x: number;
  bottom: number;
}

const stacks = (k: LabelKind): boolean => k === 'self' || k === 'player';

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
      l = { el, kind, at: new Vector3(), lift, w: 0, h: 0 };
      this.labels.set(key, l);
    }
    if (l.el.textContent !== text) {
      l.el.textContent = text;
      l.w = 0; // measured again as it's next shown
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

  /** Places every label at its point as the overlay's frame sees it, the player's name and then others' in order of arrival lifted clear of the rest; one behind the camera or off the screen hides. */
  place(): void {
    const p = this.screen;
    const shown: Placed[] = [];
    for (const l of this.labels.values()) {
      const on = l.el.textContent !== '' && this.overlay.project(l.at.x, l.at.y, l.at.z, p);
      l.el.style.display = on ? '' : 'none';
      if (on) {
        shown.push({ l, x: p.x, bottom: p.y - l.lift });
      }
    }
    for (const { l } of shown) {
      if (l.w === 0) {
        l.w = l.el.offsetWidth; // laid out once a text shows, not each frame
        l.h = l.el.offsetHeight;
      }
    }
    const box = ({ l, x, bottom }: Placed): TagBox => ({ x, bottom, w: l.w, h: l.h });
    const tags = shown
      .filter((s) => stacks(s.l.kind))
      .sort((a, b) => Number(b.l.kind === 'self') - Number(a.l.kind === 'self'));
    const fixed = shown.filter((s) => !stacks(s.l.kind)).map(box);
    stackTags(fixed, tags.map(box)).forEach((bottom, i) => (tags[i]!.bottom = bottom));
    for (const { l, x, bottom } of shown) {
      l.el.style.transform = `translate(-50%,-100%) translate(${x}px,${bottom}px)`;
    }
  }

  dispose(): void {
    this.clear();
  }
}
