import { describe, expect, it } from 'vitest';

import { FixedStep, tickTiming, ticksIn } from './fixed-step';

const at20 = tickTiming(20, 1 / 60);

describe('FixedStep', () => {
  it.each([
    { name: 'a 16.7 ms frame yields one step', frames: [16.7], want: [1] },
    { name: 'a 50 ms frame yields three steps, not two', frames: [50], want: [3] },
    {
      name: 'three 1000/60 frames yield one each',
      frames: [1000 / 60, 1000 / 60, 1000 / 60],
      want: [1, 1, 1],
    },
    { name: 'a short frame yields nothing and carries over', frames: [10, 10], want: [0, 1] },
    { name: 'a 1 s frame is capped and the rest dropped', frames: [1000, 16.7], want: [15, 1] },
    { name: 'a zero frame yields nothing', frames: [0], want: [0] },
  ])('$name', ({ frames, want }) => {
    const fs = new FixedStep(at20);
    expect(frames.map((f) => fs.advance(f).length)).toEqual(want);
  });

  it.each([
    { name: 'one step per frame', frames: Array<number>(9).fill(1000 / 60) },
    { name: 'three steps per frame', frames: [50, 50, 50] },
    { name: 'mixed frames', frames: [16.7, 33.4, 50, 8, 8.7, 100] },
  ])('every third step starts a tick across $name', ({ frames }) => {
    const fs = new FixedStep(at20);
    const flags = frames.flatMap((f) => fs.advance(f).map((s) => s.tickStart));
    expect(flags.length).toBeGreaterThan(3);
    flags.forEach((flag, k) => {
      expect(flag).toBe(k % 3 === 0);
    });
  });

  it('a long frame that hits the cap keeps the tick boundary where the steps left it', () => {
    const fs = new FixedStep(tickTiming(30, 1 / 60)); // two steps a tick, so the 15-step cap ends one tick in
    const first = fs.advance(1000);
    expect(first).toHaveLength(15);
    expect(first.at(-1)?.tickStart).toBe(true);
    expect(fs.advance(50).map((s) => s.tickStart)).toEqual([false, true, false]);
  });

  it.each([-1, Number.NaN])('refuses frame time %s', (frameMs) => {
    expect(() => new FixedStep(at20).advance(frameMs)).toThrow(RangeError);
  });

  it('at 30 Hz every second step starts a tick', () => {
    const fs = new FixedStep(tickTiming(30, 1 / 60));
    expect(fs.advance(100).map((s) => s.tickStart)).toEqual([
      true,
      false,
      true,
      false,
      true,
      false,
    ]);
  });

  it('refuses fewer than one step per tick', () => {
    expect(() => new FixedStep({ ...at20, stepsPerTick: 0 })).toThrow(RangeError);
  });

  it('steps by the movement dt it was given', () => {
    expect(new FixedStep(tickTiming(25, 1 / 50)).advance(100)).toHaveLength(5);
  });
});

describe('ticksIn', () => {
  it.each([
    { ms: 600, hz: 12, ticks: 8 },
    { ms: 800, hz: 12, ticks: 10 },
    { ms: 600, hz: 20, ticks: 12 },
    { ms: 800, hz: 30, ticks: 24 }, // 23.999… in floating point
    { ms: 1000, hz: 60, ticks: 60 },
    { ms: 0, hz: 20, ticks: 1 },
  ])('$ms ms at $hz Hz is $ticks ticks', ({ ms, hz, ticks }) => {
    expect(ticksIn(ms, 1000 / hz)).toBe(ticks);
  });
});

describe('tickTiming', () => {
  it.each([
    { hz: 20, dt: 1 / 60, tickMs: 50, stepsPerTick: 3 },
    { hz: 30, dt: 1 / 60, tickMs: 1000 / 30, stepsPerTick: 2 },
    { hz: 60, dt: 1 / 60, tickMs: 1000 / 60, stepsPerTick: 1 },
    { hz: 0, dt: 1 / 60, tickMs: 50, stepsPerTick: 3 },
    { hz: 7, dt: 1 / 60, tickMs: 50, stepsPerTick: 3 },
    { hz: 25, dt: 1 / 50, tickMs: 40, stepsPerTick: 2 },
  ])('$hz Hz paces $stepsPerTick steps of $dt s a tick', ({ hz, dt, tickMs, stepsPerTick }) => {
    expect(tickTiming(hz, dt)).toEqual({
      tickMs,
      stepsPerTick,
      stepsPerSecond: Math.round(1 / dt),
    });
  });
});
