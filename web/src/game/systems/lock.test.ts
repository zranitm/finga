import { describe, expect, it } from 'vitest';

import type { Mark } from './aim';
import { Lock } from './lock';

type Seen = 'view' | 'frame' | 'off'; // in view; on the canvas but within its margin; off it

const mark = (id: number, x: number, z: number, hp = 10, seen: Seen = 'view'): Mark => ({
  id,
  x,
  z,
  radius: 0.5,
  hp,
  inView: seen === 'view',
  inFrame: seen !== 'off',
});

describe('Lock', () => {
  it('picks the nearest living mob in view, in any direction, a tie to the lower id', () => {
    expect(new Lock().update(0, 0, [mark(1, 0, 6), mark(2, 0, -4), mark(3, 5, 0)])?.id).toBe(2); // behind
    expect(new Lock().update(0, 0, [mark(4, 3, 0), mark(2, -3, 0)])?.id).toBe(2);
    expect(
      new Lock().update(0, 0, [
        mark(1, 1, 0, 0),
        mark(2, 2, 0, 10, 'frame'),
        mark(3, 3, 0, 10, 'off'),
      ]),
    ).toBeNull(); // killed, at the edge, off screen
    expect(new Lock().update(0, 0, [mark(2, 1, 0, 10, 'frame'), mark(3, 30, 0)])?.id).toBe(3); // far but in view, past a nearer one at the edge
  });

  it('keeps its mob when another comes closer', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0), mark(2, 8, 0)]);
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 1, 0)])?.id).toBe(1);
  });

  it('keeps its mob at the screen’s edge, but drops it once off screen, dead or gone, and picks the nearest in view', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 2, 0), mark(2, 5, 0), mark(3, 7, 0)]);
    expect(lock.update(0, 0, [mark(1, 2, 0, 10, 'frame'), mark(2, 5, 0)])?.id).toBe(1); // at the edge: held
    expect(lock.update(0, 0, [mark(1, 2, 0, 10, 'off'), mark(2, 5, 0), mark(3, 7, 0)])?.id).toBe(2);
    expect(lock.update(0, 0, [mark(2, 5, 0, 0), mark(3, 7, 0)])?.id).toBe(3);
    expect(lock.update(0, 0, [mark(4, 4, 0)])?.id).toBe(4);
    expect(lock.update(0, 0, [])).toBeNull();
  });

  it('Tab moves to the next farther mob in view, behind ones too, and round to the nearest after the farthest', () => {
    const lock = new Lock();
    const mobs = [
      mark(1, 2, 0),
      mark(2, 0, -4),
      mark(3, -6, 0),
      mark(4, 0, 3, 10, 'frame'),
      mark(5, 0, 1, 10, 'off'),
    ];
    expect(lock.update(0, 0, mobs)?.id).toBe(1);
    expect(lock.next(0, 0, mobs)?.id).toBe(2);
    expect(lock.next(0, 0, mobs)?.id).toBe(3);
    expect(lock.next(0, 0, mobs)?.id).toBe(1); // the ones at the edge and off screen never come up
    expect(lock.update(0, 0, mobs)?.id).toBe(1);
  });

  it('Tab keeps a lone lock, at the screen’s edge too, picks the nearest with none, and finds nothing with none in view', () => {
    const lock = new Lock();
    expect(lock.next(0, 0, [mark(1, 3, 0), mark(2, 1, 0)])?.id).toBe(2);
    expect(lock.next(0, 0, [mark(2, 1, 0)])?.id).toBe(2);
    expect(lock.next(0, 0, [mark(2, 1, 0, 10, 'frame')])?.id).toBe(2);
    expect(lock.next(0, 0, [mark(2, 1, 0, 10, 'frame'), mark(3, 6, 0)])?.id).toBe(3); // the edge's lock moves on to one in view
    expect(new Lock().next(0, 0, [mark(2, 1, 0, 10, 'frame')])).toBeNull();
    expect(lock.next(0, 0, [])).toBeNull();
  });

  it('lets go when cleared, and the next update picks again', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0)]);
    lock.clear();
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 3, 0)])?.id).toBe(2);
  });
});
