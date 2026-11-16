// Mobs' HP bars (3d-world.md section 10): DOM over the canvas like the names, each over its mob's head, an elite's in a gold frame; a boss has the bar at the top of the screen instead.
import type { Overlay, ScreenPoint } from './overlay';

const BAR_W = 32;
const BAR_H = 4;
const LIFT_PX = 6; // its bottom over the head
const ELITE_FRAME = '#f2c14e';

interface Bar {
  readonly el: HTMLDivElement;
  readonly fill: HTMLDivElement;
  x: number;
  y: number;
  z: number;
  ratio: number;
}

export class Bars {
  private readonly bars = new Map<number, Bar>();
  private readonly screen: ScreenPoint = { x: 0, y: 0 };

  constructor(private readonly overlay: Overlay) {}

  /** Shows mob id's bar over its head at (x, y, z), filled to hp of maxHp. */
  set(
    id: number,
    x: number,
    y: number,
    z: number,
    hp: number,
    maxHp: number,
    elite: boolean,
  ): void {
    let b = this.bars.get(id);
    if (!b) {
      const el = document.createElement('div');
      const frame = elite ? `;outline:2px solid ${ELITE_FRAME}` : '';
      el.style.cssText = `position:absolute;left:0;top:0;width:${BAR_W}px;height:${BAR_H}px;background:rgba(26,26,36,0.8)${frame}`;
      el.dataset.bar = String(id);
      const fill = document.createElement('div');
      fill.style.cssText = 'height:100%;width:100%;background:#e04848';
      el.appendChild(fill);
      this.overlay.layer.prepend(el); // under the names and numbers
      b = { el, fill, x, y, z, ratio: 1 };
      this.bars.set(id, b);
    }
    const ratio = maxHp > 0 ? Math.max(0, Math.min(1, hp / maxHp)) : 0;
    if (ratio !== b.ratio) {
      b.fill.style.width = `${ratio * 100}%`;
      b.ratio = ratio;
    }
    b.x = x;
    b.y = y;
    b.z = z;
  }

  delete(id: number): void {
    this.bars.get(id)?.el.remove();
    this.bars.delete(id);
  }

  clear(): void {
    for (const b of this.bars.values()) {
      b.el.remove();
    }
    this.bars.clear();
  }

  /** Places every bar over its head as the overlay's frame sees it; one behind the camera or off the screen hides. */
  place(): void {
    const p = this.screen;
    for (const b of this.bars.values()) {
      const on = this.overlay.project(b.x, b.y, b.z, p);
      b.el.style.display = on ? '' : 'none';
      if (on) {
        b.el.style.transform = `translate(${p.x - BAR_W / 2}px,${p.y - LIFT_PX - BAR_H}px)`;
      }
    }
  }

  dispose(): void {
    this.clear();
  }
}
