import { describe, expect, it } from 'vitest';

import {
  RENDER_DELAY_MS,
  RemoteBuffer,
  RemotePlayers,
  RenderClock,
  TICK_MS,
  blend,
  sample,
} from './interpolation';
import type { RemoteWire, Timed } from './interpolation';
import type { WireState } from './prediction';

const at = (x: number, y: number, vx = 0, vy = 0, extra: Partial<WireState> = {}): WireState => ({
  x,
  y,
  vx,
  vy,
  facing: 1,
  on_ground: true,
  climbing: false,
  ...extra,
});

const buffer: Timed[] = [
  { t: 1000, state: at(100, 608, 200) },
  { t: 1050, state: at(110, 608, 200) },
  { t: 1100, state: at(120, 600, 200, -300, { on_ground: false, facing: -1 }) },
];

describe('sample', () => {
  it.each([
    { name: 'between two states returns the linear blend', time: 1025, want: at(105, 608, 200) },
    {
      name: 'a quarter of the way blends a quarter',
      time: 1062.5,
      want: { ...at(112.5, 606, 200, -75), on_ground: true, facing: 1 },
    },
    {
      name: 'past the middle takes the later flags',
      time: 1090,
      want: { ...at(118, 601.6, 200, -240), on_ground: false, facing: -1 },
    },
    { name: 'before the first state returns the first', time: 900, want: at(100, 608, 200) },
    { name: 'exactly on a state returns it', time: 1050, want: at(110, 608, 200) },
    {
      name: 'after the last extrapolates along its velocity',
      time: 1125,
      want: at(125, 592.5, 200, -300, { on_ground: false, facing: -1 }),
    },
    {
      name: 'after the last extrapolates no further than one tick',
      time: 1100 + 10 * TICK_MS,
      want: at(130, 585, 200, -300, { on_ground: false, facing: -1 }),
    },
  ])('$name', ({ time, want }) => {
    const got = sample(buffer, time)!;
    for (const k of ['x', 'y', 'vx', 'vy'] as const) {
      expect(got[k]).toBeCloseTo(want[k], 6);
    }
    expect(got.facing).toBe(want.facing);
    expect(got.on_ground).toBe(want.on_ground);
  });

  it('an empty buffer samples nothing', () => {
    expect(sample([], 1000)).toBeUndefined();
  });

  it('blend at 0 and 1 returns the ends', () => {
    expect(blend(buffer[0]!.state, buffer[2]!.state, 0)).toEqual(buffer[0]!.state);
    expect(blend(buffer[0]!.state, buffer[2]!.state, 1)).toEqual(buffer[2]!.state);
  });
});

describe('RemoteBuffer', () => {
  it('keeps states in order, replaces a repeat for the same tick, and drops states older than a second', () => {
    const b = new RemoteBuffer();
    b.push(1000, at(1, 0));
    b.push(1050, at(2, 0));
    b.push(1050, at(3, 0));
    b.push(1000, at(4, 0)); // out of order: ignored
    expect(b.size).toBe(2);
    expect(b.sample(1050)).toEqual(at(3, 0));
    b.push(2100, at(5, 0));
    expect(b.size).toBe(2); // 1000 fell out, 1050 stays as the floor
    expect(b.sample(1000)).toEqual(at(3, 0));
  });

  it('a removed player stays visible until the buffer has drained past the removal tick', () => {
    const b = new RemoteBuffer();
    b.push(1000, at(1, 0));
    b.push(1050, at(2, 0));
    b.remove(1100);
    expect(b.removed).toBe(true);
    expect(b.drained(1099)).toBe(false);
    expect(b.sample(1075)).toEqual(at(2, 0));
    expect(b.drained(1100)).toBe(true);
  });
});

const wire = (id: string, x: number, name = id, cls = 'warrior'): RemoteWire => ({
  id,
  name,
  class: cls,
  ...at(x, 608),
});

describe('RemotePlayers', () => {
  it('a Snapshot sets the map, skipping the local player, and drops anyone not in it', () => {
    const r = new RemotePlayers('me');
    r.applySnapshot(1000, [wire('me', 1), wire('a', 10, 'Ayla'), wire('b', 20, 'Bruno', 'bowman')]);
    expect(r.size).toBe(2);
    expect(r.nameOf('a')).toBe('Ayla');
    expect(r.classOf('b')).toBe('bowman');
    r.applySnapshot(1050, [wire('a', 11)]);
    expect(r.size).toBe(1);
    expect(r.render(1050).shown.get('a')?.x).toBe(11);
  });

  it('Deltas add states, and a removal is drawn until drained then reported once', () => {
    const r = new RemotePlayers('me');
    r.applySnapshot(1000, [wire('a', 10)]);
    r.applyDelta(1050, [wire('a', 20), wire('me', 5)], []);
    r.applyDelta(1100, [wire('a', 30)], []);
    r.applyDelta(1150, [], ['a']);
    expect(r.render(1075).shown.get('a')?.x).toBe(25);
    expect(r.render(1149).gone).toEqual([]);
    expect(r.render(1149).shown.get('a')?.x).toBe(30); // extrapolation: no velocity
    const { shown, gone } = r.render(1150);
    expect(gone).toEqual(['a']);
    expect(shown.size).toBe(0);
    expect(r.render(1200).gone).toEqual([]);
    expect(r.size).toBe(0);
  });

  it('a removal for an unknown player is ignored, and clear forgets everyone', () => {
    const r = new RemotePlayers('me');
    r.applyDelta(1000, [wire('a', 1)], ['nobody']);
    expect(r.size).toBe(1);
    r.clear();
    expect(r.size).toBe(0);
    expect(r.nameOf('a')).toBe('');
  });
});

describe('RenderClock', () => {
  it('sits RENDER_DELAY_MS behind the newest tick and advances with local time between messages', () => {
    const c = new RenderClock();
    expect(c.renderTime(0)).toBeNull();
    c.update(100, 5000);
    expect(c.renderTime(5000)).toBe(100 * TICK_MS - RENDER_DELAY_MS);
    expect(c.renderTime(5030)).toBe(100 * TICK_MS - RENDER_DELAY_MS + 30);
    c.update(101, 5050);
    expect(c.renderTime(5050)).toBe(101 * TICK_MS - RENDER_DELAY_MS);
    c.update(99, 5060); // an older tick does not move the clock back
    expect(c.renderTime(5060)).toBe(101 * TICK_MS - RENDER_DELAY_MS + 10);
    c.reset();
    expect(c.renderTime(5100)).toBeNull();
  });
});
