import { describe, expect, it } from 'vitest';

import { aimAt, aimYaw, reachOf } from './aim';
import type { Mark } from './aim';

const EAST = Math.PI / 2;
const mark = (id: number, x: number, z: number, radius = 0.5, hp = 10): Mark => ({
  id,
  x,
  z,
  radius,
  hp,
});

describe('auto-aim', () => {
  it('reaches as far as the action: an arc’s radius, a shot’s range, an area’s radius; a heal aims at no one', () => {
    expect(reachOf({ kind: 'arc', radius: 2.375 })).toBe(2.375);
    expect(reachOf({ kind: 'shot', range: 13.125 })).toBe(13.125);
    expect(reachOf({ kind: 'area', radius: 1.5 })).toBe(1.5);
    expect(reachOf({ kind: 'area', radius: 4.6875 }, true)).toBe(0);
    expect(reachOf(null)).toBe(0);
  });

  it('picks the nearest living mob whose body it reaches, within 70° of the heading', () => {
    const mobs = [
      mark(1, 2.8, 0), // 2.8 m east
      mark(2, 2, 1.5), // 2.5 m, 37° off
      mark(3, 1, -2, 0.5, 0), // dead
      mark(4, 0, 1.5), // 90° off
      mark(5, 6, 0), // beyond its reach
    ];
    expect(aimAt(0, 0, EAST, 2.375, mobs)?.id).toBe(2);
    expect(aimAt(0, 0, EAST, 2.375, mobs.slice(0, 1))?.id).toBe(1); // reach 2.375 plus its radius 0.5 meets it by 2.875
    expect(aimAt(0, 0, EAST, 2.375, [mark(5, 6, 0)])).toBeNull();
    expect(aimAt(0, 0, EAST, 2.375, [mark(4, 0, 1.5)])).toBeNull();
  });

  it('gives a tie to the mob nearer the heading', () => {
    const off = 2 * Math.cos(Math.PI / 6);
    const mobs = [mark(1, off, 1), mark(2, 2, 0), mark(3, off, -1)];
    expect(aimAt(0, 0, EAST, 3, mobs)?.id).toBe(2);
  });

  it('turns to the mark, or keeps the heading with none', () => {
    expect(aimYaw(0, 0, EAST, mark(1, 0, -2))).toBeCloseTo(Math.PI);
    expect(aimYaw(0, 0, EAST, null)).toBe(EAST);
    expect(aimYaw(1, 1, EAST, mark(1, 1, 1))).toBe(EAST);
  });
});
