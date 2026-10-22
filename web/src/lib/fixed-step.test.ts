import { describe, expect, it } from 'vitest';

import { FixedStep } from './fixed-step';

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
    const fs = new FixedStep();
    expect(frames.map((f) => fs.advance(f).length)).toEqual(want);
  });

  it.each([
    { name: 'one step per frame', frames: Array<number>(9).fill(1000 / 60) },
    { name: 'three steps per frame', frames: [50, 50, 50] },
    { name: 'mixed frames', frames: [16.7, 33.4, 50, 8, 8.7, 100] },
  ])('every third step starts a tick across $name', ({ frames }) => {
    const fs = new FixedStep();
    const flags = frames.flatMap((f) => fs.advance(f).map((s) => s.tickStart));
    expect(flags.length).toBeGreaterThan(3);
    flags.forEach((flag, k) => {
      expect(flag).toBe(k % 3 === 0);
    });
  });

  it('a long frame that hits the cap keeps the tick boundary where the steps left it', () => {
    const fs = new FixedStep(4); // 4 steps: the 5th would have started a tick
    const first = fs.advance(1000);
    expect(first.map((s) => s.tickStart)).toEqual([true, false, false, true]);
    expect(fs.advance(50).map((s) => s.tickStart)).toEqual([false, false, true]);
  });

  it.each([-1, Number.NaN])('refuses frame time %s', (frameMs) => {
    expect(() => new FixedStep().advance(frameMs)).toThrow(RangeError);
  });

  it('refuses a cap below one', () => {
    expect(() => new FixedStep(0)).toThrow(RangeError);
  });

  it('exposes the step length for the simulation', () => {
    expect(FixedStep.stepMs).toBeCloseTo(16.6667, 3);
  });
});
