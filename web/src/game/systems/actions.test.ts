import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { actions } from '@/gamedata/actions';
import { skillById } from '@/progression/skills';
import { StatSheetSchema } from '@/protocol/messages';

import { ActionClock, attackTiming, skillTiming } from './actions';
import type { ActionTiming } from './actions';

// At 50 ms ticks, an attack of 12 ticks has its cancel point at 500 ms and ends at 600; a skill of 21 at 850 and 1050. A press counts from early ms before a cancel point.
const early = actions.early_press_ms;
const attack: ActionTiming = { kind: 'attack', ticks: 12, hit: 0, cancel: 0.85, cancelable: true };
const skill = (cancelable: boolean): ActionTiming => ({
  kind: 'skill',
  ticks: 21,
  hit: 0,
  cancel: 0.85,
  cancelable,
});

/** A clock at 50 ms ticks, from time 0, counting cancels. */
function clock() {
  let t = 0;
  const cancels = vi.fn();
  const c = new ActionClock(50, () => t, cancels);
  const again = vi.fn();
  return {
    c,
    again,
    cancels,
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

  it('a cancel is an action of the other kind starting before the running one ends; nothing else is', () => {
    const { c, cancels, at } = clock();
    c.begin(attack); // none running
    at(500); // the attack's cancel point
    c.begin(skill(true));
    expect(cancels).toHaveBeenCalledTimes(1);
    at(500 + 1050 - 1);
    c.begin(attack); // a moment before the skill's end
    expect(cancels).toHaveBeenCalledTimes(2);
    at(500 + 1050 - 1 + 600);
    c.begin(skill(true)); // the attack ended at 600
    c.begin(skill(true)); // the same kind
    expect(cancels).toHaveBeenCalledTimes(2);
  });

  it('a press with no action running goes at once', () => {
    const { c, again } = clock();
    expect(c.press('attack', again)).toBe(true);
    expect(c.press('skill', again)).toBe(true);
  });

  it('a skill pressed within the early press of an attack’s cancel point goes at it; one just outside waits for the end', () => {
    const { c, again, at } = clock();
    c.begin(attack);
    at(500 - early - 1);
    expect(c.press('skill', again)).toBe(false);
    at(599);
    expect(again).not.toHaveBeenCalled();
    at(600);
    expect(again).toHaveBeenCalledTimes(1);
    c.begin(attack);
    at(1100 - early); // this attack's cancel point is at 1100
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
    'an attack pressed any time into a skill cancelable: $cancelable, even at its start, goes at $goes',
    ({ cancelable, goes }) => {
      const { c, again, at } = clock();
      c.begin(skill(cancelable));
      at(50);
      expect(c.press('attack', again)).toBe(false);
      at(goes - 1);
      expect(again).not.toHaveBeenCalled();
      at(goes);
      expect(again).toHaveBeenCalledTimes(1);
    },
  );

  it('a newer press takes the held one’s place, and a reset forgets it', () => {
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
    c.reset();
    at(1200);
    expect(first).not.toHaveBeenCalled();
    expect(c.canStart('attack')).toBe(true);
  });

  it('while a skill press waits, an attack press does nothing; a skill press still takes a waiting attack’s place, and a newer skill a waiting skill’s', () => {
    const { c, at } = clock();
    const skill1 = vi.fn();
    const tap = vi.fn();
    const skill2 = vi.fn();
    c.begin(attack);
    at(100);
    c.press('skill', skill1); // too early to cut the attack: it waits for the end
    expect(c.press('attack', tap)).toBe(false);
    at(600);
    expect([skill1.mock.calls.length, tap.mock.calls.length]).toEqual([1, 0]);
    c.begin(attack);
    at(700);
    c.press('attack', tap);
    c.press('skill', skill1);
    c.press('skill', skill2);
    at(1200);
    expect([skill1.mock.calls.length, tap.mock.calls.length, skill2.mock.calls.length]).toEqual([
      1, 0, 1,
    ]);
  });

  it('a held attack key’s repeat runs as an attack’s end comes, once for that moment, and a reset forgets it', () => {
    const { c, at } = clock();
    const go = vi.fn();
    c.repeatWhenFree(go); // nothing running: nothing to wait for
    c.begin(attack);
    at(100);
    c.repeatWhenFree(go);
    at(300);
    c.repeatWhenFree(go); // the same moment: still one
    at(599);
    expect(go).not.toHaveBeenCalled();
    at(600);
    expect(go).toHaveBeenCalledTimes(1);
    c.begin(attack);
    c.repeatWhenFree(go);
    c.reset();
    at(1300);
    expect(go).toHaveBeenCalledTimes(1);
  });

  it.each([
    { cancelable: true, goes: 850 },
    { cancelable: false, goes: 1050 },
  ])(
    'a held attack key repeats into a skill cancelable: $cancelable at $goes, and may start from then',
    ({ cancelable, goes }) => {
      const { c, at } = clock();
      const go = vi.fn();
      expect(c.canStart('attack')).toBe(true);
      c.begin(skill(cancelable));
      at(50);
      c.repeatWhenFree(go);
      at(goes - 1);
      expect([go.mock.calls.length, c.canStart('attack')]).toEqual([0, false]);
      at(goes);
      expect([go.mock.calls.length, c.canStart('attack')]).toEqual([1, true]);
    },
  );

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
      ticks: 30,
      hit: 0,
      cancel: 0.3, // just after its strike, so an attack frees the player from its slow early
      cancelable: true,
    });
    expect(skillTiming(skillById('heal')!.stages[0]!, 50)).toMatchObject({ cancelable: false });
  });
});
