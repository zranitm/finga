import { describe, expect, it } from 'vitest';

import { actions } from '@/gamedata/actions';

import type { Mark } from './aim';
import { Lock } from './lock';

const mark = (id: number, x: number, z: number, hp = 10): Mark => ({ id, x, z, radius: 0.5, hp });
const RANGE = actions.lock_range;

describe('Lock', () => {
  it('picks the nearest living mob within range, in any direction, a tie to the lower id', () => {
    const lock = new Lock();
    expect(lock.update(0, 0, [mark(1, 0, 6), mark(2, 0, -4), mark(3, 5, 0)])?.id).toBe(2); // behind
    expect(new Lock().update(0, 0, [mark(4, 3, 0), mark(2, -3, 0)])?.id).toBe(2);
    expect(new Lock().update(0, 0, [mark(1, 1, 0, 0), mark(2, RANGE + 0.01, 0)])).toBeNull(); // killed, and just out of range
    expect(new Lock().update(0, 0, [mark(2, RANGE, 0)])?.id).toBe(2); // its centre at the range
  });

  it('keeps its mob when another comes closer', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0), mark(2, 8, 0)]);
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 1, 0)])?.id).toBe(1);
  });

  it('drops its mob when it dies, leaves range or is gone, and picks the nearest again', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 2, 0), mark(2, 5, 0), mark(3, 7, 0)]);
    expect(lock.update(0, 0, [mark(1, 2, 0, 0), mark(2, 5, 0), mark(3, 7, 0)])?.id).toBe(2);
    expect(lock.update(0, 0, [mark(2, RANGE + 1, 0), mark(3, 7, 0)])?.id).toBe(3);
    expect(lock.update(0, 0, [mark(2, 4, 0)])?.id).toBe(2);
    expect(lock.update(0, 0, [])).toBeNull();
  });

  it('Tab moves to the next farther mob, behind ones too, and round to the nearest after the farthest', () => {
    const lock = new Lock();
    const mobs = [mark(1, 2, 0), mark(2, 0, -4), mark(3, -6, 0), mark(4, 0, RANGE + 2)];
    expect(lock.update(0, 0, mobs)?.id).toBe(1);
    expect(lock.next(0, 0, mobs)?.id).toBe(2);
    expect(lock.next(0, 0, mobs)?.id).toBe(3);
    expect(lock.next(0, 0, mobs)?.id).toBe(1); // the one out of range never comes up
    expect(lock.update(0, 0, mobs)?.id).toBe(1);
  });

  it('Tab keeps a lone lock, picks the nearest with none, and finds nothing with none in range', () => {
    const lock = new Lock();
    expect(lock.next(0, 0, [mark(1, 3, 0), mark(2, 1, 0)])?.id).toBe(2);
    expect(lock.next(0, 0, [mark(2, 1, 0)])?.id).toBe(2);
    expect(lock.next(0, 0, [])).toBeNull();
  });

  it('lets go when cleared, and the next update picks again', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0)]);
    lock.clear();
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 3, 0)])?.id).toBe(2);
  });
});
