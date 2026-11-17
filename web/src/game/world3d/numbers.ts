// Damage numbers (combat-effects.md section 4): DOM text at a world point, stacked where it starts on the screen, rising, popping and fading there.
import { SHIELD } from '../effects/colors';
import { DamageNumbers } from '../systems/combat-view';
import type { NumberKind } from '../systems/combat-view';

import type { Overlay, ScreenPoint } from './overlay';

const OUTLINE = 'text-shadow:0 0 3px #0a0c16,0 0 2px #0a0c16,0 0 1px #0a0c16';
const CRIT = 1.3; // a crit's number is this much bigger

const STYLE: Readonly<Record<NumberKind, string>> = {
  hit: 'color:#f4f4f8;font:700 15px sans-serif',
  crit: `color:#ffd166;font:700 ${Math.round(15 * CRIT)}px sans-serif`,
  miss: 'color:#b0b0c4;font:600 14px sans-serif',
  taken: 'color:#ff5a5a;font:700 15px sans-serif',
  healHp: 'color:#6fdc6f;font:700 15px sans-serif',
  dot: 'color:#f4f4f8;font:600 12px sans-serif', // small, in its status's colour as spawned
  shield: `color:#${SHIELD.toString(16)};font:700 14px sans-serif`,
};

interface Shown {
  readonly el: HTMLDivElement;
  readonly x: number;
  readonly y: number;
  readonly z: number;
}

export class NumberLayer {
  private readonly queue = new DamageNumbers();
  private readonly shown = new Map<number, Shown>();
  private readonly screen: ScreenPoint = { x: 0, y: 0 };

  constructor(private readonly overlay: Overlay) {}

  /** A number over world point (x, y, z), stacked by where it starts on the screen, in color (0xrrggbb) over its kind's if given; one off the screen isn't shown. */
  spawn(
    x: number,
    y: number,
    z: number,
    text: string,
    kind: NumberKind,
    now: number,
    color?: number,
  ): void {
    const p = this.screen;
    if (!this.overlay.project(x, y, z, p)) {
      return;
    }
    const n = this.queue.spawn(p.x, p.y, text, kind, now);
    const el = document.createElement('div');
    const own = color === undefined ? '' : `;color:#${color.toString(16).padStart(6, '0')}`;
    el.style.cssText = `position:absolute;left:0;top:0;white-space:nowrap;transform-origin:0 0;${OUTLINE};${STYLE[kind]}${own}`;
    el.dataset.number = kind;
    el.textContent = text;
    this.overlay.layer.appendChild(el);
    this.shown.set(n.id, { el, x, y, z });
  }

  /** Draws each number at its point as the overlay's frame sees it, risen, popped and faded for now; the ones done go. */
  place(now: number): void {
    const p = this.screen;
    const live = new Set<number>();
    for (const n of this.queue.update(now)) {
      const s = this.shown.get(n.id);
      if (!s) {
        continue;
      }
      live.add(n.id);
      const on = this.overlay.project(s.x, s.y, s.z, p);
      s.el.style.display = on ? '' : 'none';
      if (on) {
        s.el.style.opacity = String(n.alpha);
        s.el.style.transform = `translate(${p.x}px,${p.y - n.lift + n.dy}px) scale(${n.scale}) translate(-50%,-100%)`; // grows from its bottom middle
      }
    }
    for (const [id, s] of this.shown) {
      if (!live.has(id)) {
        s.el.remove();
        this.shown.delete(id);
      }
    }
  }

  clear(): void {
    this.queue.clear();
    for (const s of this.shown.values()) {
      s.el.remove();
    }
    this.shown.clear();
  }
}
