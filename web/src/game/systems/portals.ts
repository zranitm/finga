import type { Portal } from '@/sim/map';

/** Edges included, as on the server. */
export function standsIn(x: number, y: number, portals: readonly Portal[]): Portal | undefined {
  return portals.find(
    (p) => x >= p.area.x && x <= p.area.x + p.area.w && y >= p.area.y && y <= p.area.y + p.area.h,
  );
}

export class PortalSystem {
  private held = false;
  private armed = true;

  constructor(private readonly send: () => boolean) {}

  /** Only a fresh press sends EnterPortal; holding the key sends nothing more. */
  keyDown(inPortal: boolean): boolean {
    if (this.held) {
      return false;
    }
    this.held = true;
    if (!this.armed) {
      return false;
    }
    this.armed = false;
    return inPortal && this.send();
  }

  keyUp(): void {
    this.held = false;
    this.armed = true;
  }

  /** You arrive standing in the target portal, so a held key must be released before it counts again. */
  arrived(): void {
    this.armed = false;
  }

  get isHeld(): boolean {
    return this.held;
  }
}
