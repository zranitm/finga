import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { skillById } from '@/progression/skills';
import { StatSheetSchema } from '@/protocol/messages';

import { ActionClock, attackTiming, skillTiming } from './actions';
import type { ActionTiming } from './actions';

// At 50 ms ticks, an attack of 12 ticks has its cancel point at 500 ms and ends at 600; a skill of 21 at 850 and 1050.
const attack: ActionTiming = { kind: 'attack', ticks: 12, hit: 0, cancel: 0.85, cancelable: true };
const skill = (cancelable: boolean): ActionTiming => ({
  kind: 'skill',
  ticks: 21,
  hit: 0,
  cancel: 0.85,
  cancelable,
});

/** A clock at 50 ms ticks with a 100 ms early press, from time 0. */
function clock() {
  let t = 0;
  const c = new ActionClock(50, () => t, 100);
  const again = vi.fn();
  return {
    c,
    again,
    at: (ms: number) => {
      vi.advanceTimersByTime(ms - t);
      t = ms;
    },
  };
}

describe('ActionClock', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('a press with no action running goes at once', () => {
    const { c, again } = clock();
    expect(c.press('attack', again)).toBe(true);
    expect(c.press('skill', again)).toBe(true);
  });

  it('a skill pressed within the early press of an attack’s cancel point goes at it; one just outside waits for the end', () => {
    const { c, again, at } = clock();
    c.begin(attack);
    at(399);
    expect(c.press('skill', again)).toBe(false);
    at(499);
    expect(again).not.toHaveBeenCalled();
    at(600);
    expect(again).toHaveBeenCalledTimes(1);
    c.begin(attack);
    at(1000); // 100 ms before this attack's cancel point at 1100
    expect(c.press('skill', again)).toBe(false);
    expect(c.waiting()).toBe(true);
    at(1100);
    expect([again.mock.calls.length, c.waiting()]).toEqual([2, false]);
  });

  it('a skill pressed past the attack’s cancel point goes at once', () => {
    const { c, again, at } = clock();
    c.begin(attack);
    at(500);
    expect(c.press('skill', again)).toBe(true);
  });

  it('the same kind waits for the end, cancel point or not', () => {
    const { c, again, at } = clock();
    c.begin(attack);
    at(550);
    expect(c.press('attack', again)).toBe(false);
    at(599);
    expect(again).not.toHaveBeenCalled();
    at(600);
    expect(again).toHaveBeenCalledTimes(1);
    c.begin(skill(true));
    at(1500);
    expect(c.press('skill', again)).toBe(false);
    at(1650);
    expect(again).toHaveBeenCalledTimes(2);
  });

  it.each([
    { cancelable: true, goes: 850 },
    { cancelable: false, goes: 1050 },
  ])(
    'an attack pressed at 800 into a skill cancelable: $cancelable goes at $goes',
    ({ cancelable, goes }) => {
      const { c, again, at } = clock();
      c.begin(skill(cancelable));
      at(800);
      expect(c.press('attack', again)).toBe(false);
      at(goes - 1);
      expect(again).not.toHaveBeenCalled();
      at(goes);
      expect(again).toHaveBeenCalledTimes(1);
    },
  );

  it('a newer press takes the held one’s place, and stop and reset forget it', () => {
    const { c, at } = clock();
    const first = vi.fn();
    const second = vi.fn();
    c.begin(attack);
    at(100);
    c.press('attack', first);
    c.press('skill', second); // too early to cancel: it waits for the end too
    at(600);
    expect([first.mock.calls.length, second.mock.calls.length]).toEqual([0, 1]);
    c.begin(attack);
    c.press('attack', first);
    c.stop();
    c.press('attack', first);
    c.reset();
    at(1200);
    expect(first).not.toHaveBeenCalled();
    expect(c.idle()).toBe(true);
  });

  it('a held attack key finds the clock idle only once the action ends', () => {
    const { c, at } = clock();
    expect(c.idle()).toBe(true);
    c.begin(skill(true));
    at(1049);
    expect(c.idle()).toBe(false);
    at(1050);
    expect(c.idle()).toBe(true);
  });

  it('an undo gives the action back unless another started since', () => {
    const { c, again, at } = clock();
    c.begin(attack);
    at(500);
    const undo = c.begin(skill(false));
    undo();
    expect(c.press('skill', again)).toBe(true); // the attack is back, past its cancel point
    const late = c.begin(skill(false));
    at(1550);
    c.begin(attack);
    late();
    expect(c.press('attack', again)).toBe(false); // the attack started since stands
  });
});

describe('action timings', () => {
  it.each([
    { name: 'the sheet’s count', ticks: 11, hz: 20, cls: 'warrior', want: 11 },
    {
      name: 'before a sheet, the class’s 900 ms in whole ticks at 12 Hz',
      ticks: 0,
      hz: 12,
      cls: 'warrior',
      want: 11,
    },
    { name: 'and at 20 Hz', ticks: 0, hz: 20, cls: 'warrior', want: 18 },
    { name: 'a bowman’s 1125 ms at 12 Hz', ticks: 0, hz: 12, cls: 'bowman', want: 14 },
  ])('a basic attack runs $name', ({ ticks, hz, cls, want }) => {
    const sheet = ticks > 0 ? create(StatSheetSchema, { attackLengthTicks: ticks }) : undefined;
    expect(attackTiming(sheet, cls, 1000 / hz)).toEqual({
      kind: 'attack',
      ticks: want,
      hit: 0,
      cancel: 0.85,
      cancelable: true,
    });
  });

  it('a skill runs its stage’s length in whole ticks, with its shares and whether it is cancelable', () => {
    const strike = skillById('strike')!;
    expect(skillTiming(strike.stages[0]!, 50)).toEqual({
      kind: 'skill',
      ticks: 21,
      hit: 0,
      cancel: 0.85,
      cancelable: true,
    });
    expect(skillTiming(skillById('heal')!.stages[0]!, 50)).toMatchObject({ cancelable: false });
  });
});
