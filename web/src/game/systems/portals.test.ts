import { describe, expect, it, vi } from 'vitest';

import type { Portal } from '@/sim/map';

import { PortalSystem, standsIn } from './portals';

const toForest: Portal = {
  name: 'to_forest',
  area: { x: 1200, y: 544, w: 32, h: 64 },
  target_map: 'forest',
  target_portal: 'from_town',
};

describe('standsIn', () => {
  it.each([
    { name: 'inside', x: 1216, y: 600, want: 'to_forest' },
    { name: 'on the bottom edge (standing on the ground)', x: 1216, y: 608, want: 'to_forest' },
    { name: 'on the left edge', x: 1200, y: 600, want: 'to_forest' },
    { name: 'on the right edge', x: 1232, y: 600, want: 'to_forest' },
    { name: 'just past the right edge', x: 1233, y: 600, want: undefined },
    { name: 'just below the bottom edge', x: 1216, y: 609, want: undefined },
    { name: 'above the top', x: 1216, y: 543, want: undefined },
  ])('$name', ({ x, y, want }) => {
    expect(standsIn(x, y, [toForest])?.name).toBe(want);
  });

  it('no portals, no match', () => {
    expect(standsIn(1216, 600, [])).toBeUndefined();
  });
});

describe('PortalSystem', () => {
  it('a press with the feet inside the rectangle sends EnterPortal; outside sends nothing', () => {
    const send = vi.fn(() => true);
    const p = new PortalSystem(send);
    expect(p.keyDown(false)).toBe(false);
    p.keyUp();
    expect(p.keyDown(true)).toBe(true);
    expect(send).toHaveBeenCalledTimes(1);
  });

  it('holding the key sends exactly one', () => {
    const send = vi.fn(() => true);
    const p = new PortalSystem(send);
    expect(p.keyDown(true)).toBe(true);
    expect(p.keyDown(true)).toBe(false); // key repeat
    expect(p.keyDown(true)).toBe(false);
    expect(p.isHeld).toBe(true);
    expect(send).toHaveBeenCalledTimes(1);
    p.keyUp();
    expect(p.keyDown(true)).toBe(true);
    expect(send).toHaveBeenCalledTimes(2);
  });

  it('after a MapChange, a key still held sends nothing until it is released and pressed again', () => {
    const send = vi.fn(() => true);
    const p = new PortalSystem(send);
    p.keyDown(true);
    p.arrived(); // standing in the target portal, key still down
    expect(p.keyDown(true)).toBe(false);
    expect(send).toHaveBeenCalledTimes(1);
    p.keyUp();
    expect(p.keyDown(true)).toBe(true);
    expect(send).toHaveBeenCalledTimes(2);
  });

  it('an arrival with the key up leaves the next fresh press counting', () => {
    const send = vi.fn(() => true);
    const p = new PortalSystem(send);
    p.arrived();
    expect(p.keyDown(true)).toBe(false); // the arrival disarmed; this press re-arms on release
    p.keyUp();
    expect(p.keyDown(true)).toBe(true);
  });

  it('a press outside still uses up the press: moving into the portal while held sends nothing', () => {
    const send = vi.fn(() => true);
    const p = new PortalSystem(send);
    expect(p.keyDown(false)).toBe(false);
    expect(p.keyDown(true)).toBe(false);
    expect(send).not.toHaveBeenCalled();
  });
});
