import { describe, expect, it } from 'vitest';

import type { Mark } from './aim';
import { Lock } from './lock';

type Seen = 'view' | 'frame' | 'off'; // in view; on the canvas but within its margin; off it

const mark = (id: number | string, x: number, z: number, hp = 10, seen: Seen = 'view'): Mark => ({
  id: typeof id === 'number' ? `m:${id}` : id,
  x,
  z,
  radius: 0.5,
  hp,
  inView: seen === 'view',
  inFrame: seen !== 'off',
});

describe('Lock', () => {
  it('picks the nearest living mob in view, in any direction, a tie in ref order', () => {
    expect(new Lock().update(0, 0, [mark(1, 0, 6), mark(2, 0, -4), mark(3, 5, 0)])?.id).toBe('m:2'); // behind
    expect(new Lock().update(0, 0, [mark(4, 3, 0), mark(2, -3, 0)])?.id).toBe('m:2');
    expect(
      new Lock().update(0, 0, [
        mark(1, 1, 0, 0),
        mark(2, 2, 0, 10, 'frame'),
        mark(3, 3, 0, 10, 'off'),
      ]),
    ).toBeNull(); // killed, at the edge, off screen
    expect(new Lock().update(0, 0, [mark(2, 1, 0, 10, 'frame'), mark(3, 30, 0)])?.id).toBe('m:3'); // far but in view, past a nearer one at the edge
  });

  it('keeps its mob when another comes closer', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0), mark(2, 8, 0)]);
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 1, 0)])?.id).toBe('m:1');
  });

  it('keeps its mob at the screen’s edge, but drops it once off screen, dead or gone, and picks the nearest in view', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 2, 0), mark(2, 5, 0), mark(3, 7, 0)]);
    expect(lock.update(0, 0, [mark(1, 2, 0, 10, 'frame'), mark(2, 5, 0)])?.id).toBe('m:1'); // at the edge: held
    expect(lock.update(0, 0, [mark(1, 2, 0, 10, 'off'), mark(2, 5, 0), mark(3, 7, 0)])?.id).toBe(
      'm:2',
    );
    expect(lock.update(0, 0, [mark(2, 5, 0, 0), mark(3, 7, 0)])?.id).toBe('m:3');
    expect(lock.update(0, 0, [mark(4, 4, 0)])?.id).toBe('m:4');
    expect(lock.update(0, 0, [])).toBeNull();
  });

  it('Tab works outward through the mobs in view, behind ones too, and round to the nearest after the farthest', () => {
    const lock = new Lock();
    const mobs = [
      mark(1, 2, 0),
      mark(2, 0, -4),
      mark(3, -6, 0),
      mark(4, 0, 3, 10, 'frame'),
      mark(5, 0, 1, 10, 'off'),
    ];
    expect(lock.update(0, 0, mobs)?.id).toBe('m:1');
    expect(lock.next(0, 0, mobs)?.id).toBe('m:2');
    expect(lock.next(0, 0, mobs)?.id).toBe('m:3');
    expect(lock.next(0, 0, mobs)?.id).toBe('m:1'); // the ones at the edge and off screen never come up
    expect(lock.update(0, 0, mobs)?.id).toBe('m:1');
  });

  it('Tab keeps a lone lock, at the screen’s edge too, picks the nearest with none, and finds nothing with none in view', () => {
    const lock = new Lock();
    expect(lock.next(0, 0, [mark(1, 3, 0), mark(2, 1, 0)])?.id).toBe('m:2');
    expect(lock.next(0, 0, [mark(2, 1, 0)])?.id).toBe('m:2');
    expect(lock.next(0, 0, [mark(2, 1, 0, 10, 'frame')])?.id).toBe('m:2');
    expect(lock.next(0, 0, [mark(2, 1, 0, 10, 'frame'), mark(3, 6, 0)])?.id).toBe('m:3'); // the edge's lock moves on to one in view
    expect(new Lock().next(0, 0, [mark(2, 1, 0, 10, 'frame')])).toBeNull();
    expect(lock.next(0, 0, [])).toBeNull();
  });

  it('holds an enemy player as it holds a mob, and Tab goes through both, past a downed one', () => {
    const lock = new Lock();
    const units = [mark(1, 2, 0), mark('p:ann', 3, 0), mark('p:bo', 4, 0, 0), mark(2, 5, 0)];
    expect(lock.update(0, 0, units)?.id).toBe('m:1');
    expect(lock.next(0, 0, units)?.id).toBe('p:ann');
    expect(lock.update(0, 0, [mark(1, 1, 0), mark('p:ann', 3, 0)])?.id).toBe('p:ann');
    expect(lock.next(0, 0, units)?.id).toBe('m:2');
  });

  describe('Tab works outward through a pack', () => {
    const pack = (a: number, b: number, c: number) => [mark(1, a, 0), mark(2, b, 0), mark(3, c, 0)];

    it('is not bounced between the two nearest by a moving pack, and reaches the third', () => {
      const lock = new Lock();
      lock.update(0, 0, pack(1, 2, 3));
      expect(lock.next(0, 0, pack(1, 2, 3))?.id).toBe('m:2');
      expect(lock.next(0, 0, pack(2.2, 2, 3))?.id).toBe('m:3'); // the two swapped places: a re-sort would go back to m:1
    });

    it('goes first to the nearest when the sticky lock is no longer it', () => {
      const lock = new Lock();
      lock.update(0, 0, pack(1, 2, 3));
      expect(lock.next(0, 0, pack(2.5, 1, 3))?.id).toBe('m:2'); // m:2 came closer than the lock, m:1: the old Tab went on to m:3 past it
    });

    it('takes each in turn, then round to the nearest other than the lock, and on again', () => {
      const lock = new Lock();
      const mobs = pack(1, 2, 3);
      expect(lock.update(0, 0, mobs)?.id).toBe('m:1');
      expect([1, 2, 3, 4, 5, 6].map(() => lock.next(0, 0, mobs)?.id)).toEqual([
        'm:2',
        'm:3',
        'm:1', // every one locked: a new run from m:3, to the nearest other than it
        'm:2', // the rest of that run
        'm:1', // every one locked again: a new run from m:2, to the nearest other than it
        'm:3',
      ]);
    });

    it('takes a mob that ran in close during the run next', () => {
      const lock = new Lock();
      lock.update(0, 0, pack(1, 2, 3));
      expect(lock.next(0, 0, pack(1, 2, 3))?.id).toBe('m:2');
      expect(lock.next(0, 0, [...pack(1, 2, 3), mark(4, 0.5, 0)])?.id).toBe('m:4');
    });

    it('starts again from the lock after an action went out, or a lock lost', () => {
      const mobs = pack(1, 2, 3);
      const acted = new Lock();
      acted.update(0, 0, mobs);
      acted.next(0, 0, mobs);
      acted.next(0, 0, mobs); // m:3 locked, the run holds all three
      acted.endRun();
      expect(acted.next(0, 0, mobs)?.id).toBe('m:1'); // the nearest other than m:3, not the rest of a run
      expect(acted.next(0, 0, mobs)?.id).toBe('m:2');
      const lost = new Lock();
      lost.update(0, 0, mobs);
      lost.next(0, 0, mobs); // m:2
      const after = [mark(1, 1, 0), mark(2, 2, 0, 0), mark(3, 3, 0)];
      expect(lost.update(0, 0, after)?.id).toBe('m:1'); // held, dead: dropped, the nearest picked
      expect(lost.next(0, 0, after)?.id).toBe('m:3'); // the nearest other than the lock, not the rest of the old run
    });

    it('an ended run does not remember the old one: Tab goes to the nearest other than the lock', () => {
      const lock = new Lock();
      lock.update(0, 0, pack(1, 2, 3));
      lock.next(0, 0, pack(1, 2, 3)); // m:2
      lock.update(
        0,
        0,
        pack(1, 2, 3).filter((m) => m.id !== 'm:2'),
      ); // m:2 gone: the nearest, m:1, is picked afresh
      expect(
        lock.next(
          0,
          0,
          pack(1, 2, 3).filter((m) => m.id !== 'm:2'),
        )?.id,
      ).toBe('m:3');
    });

    it('lets go of the run when cleared', () => {
      const lock = new Lock();
      lock.update(0, 0, pack(1, 2, 3));
      lock.next(0, 0, pack(1, 2, 3));
      lock.clear();
      expect(lock.next(0, 0, pack(1, 2, 3))?.id).toBe('m:1'); // nothing locked: the nearest
    });
  });

  it('lets go when cleared, and the next update picks again', () => {
    const lock = new Lock();
    lock.update(0, 0, [mark(1, 5, 0)]);
    lock.clear();
    expect(lock.update(0, 0, [mark(1, 5, 0), mark(2, 3, 0)])?.id).toBe('m:2');
  });
});
