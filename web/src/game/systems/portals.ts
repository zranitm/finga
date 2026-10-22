// Portals (C12) over plain data: which portal the feet stand in (page 5's
// rule, the server's standsIn: edges included), and the up key's fresh
// press that sends EnterPortal once. Holding the key sends nothing more,
// and after a MapChange the key must be released before it counts again,
// since you arrive standing in the target portal.
import type { Portal } from '@/sim/map';

/** The portal whose rectangle contains the feet point, edges included, or undefined. */
export function standsIn(x: number, y: number, portals: readonly Portal[]): Portal | undefined {
  return portals.find(
    (p) => x >= p.area.x && x <= p.area.x + p.area.w && y >= p.area.y && y <= p.area.y + p.area.h,
  );
}

export class PortalSystem {
  private held = false;
  private armed = true; // a fresh press counts; cleared by a press and by arrival

  constructor(private readonly send: () => boolean) {}

  /** The up key went down; true when an EnterPortal went out. */
  keyDown(inPortal: boolean): boolean {
    if (this.held) {
      return false; // key repeat, or a key already down
    }
    this.held = true;
    if (!this.armed) {
      return false;
    }
    this.armed = false;
    return inPortal && this.send();
  }

  /** The up key went up: the next press is fresh. */
  keyUp(): void {
    this.held = false;
    this.armed = true;
  }

  /** A MapChange landed: a key still held counts only after it is released and pressed again. */
  arrived(): void {
    this.armed = false;
  }

  get isHeld(): boolean {
    return this.held;
  }
}
