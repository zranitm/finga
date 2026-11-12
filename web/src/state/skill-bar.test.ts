import { beforeEach, describe, expect, it } from 'vitest';

import { skillBar } from './skill-bar';

const s = () => skillBar.getState();

describe('skill bar', () => {
  beforeEach(() => skillBar.reset());

  it('loads six keys, padding a short bar, without counting it as a change to save', () => {
    s().load(['strike', 'spark']);
    expect(s().slots).toEqual(['strike', 'spark', '', '', '', '']);
    expect(s().edits).toBe(0);
  });

  it('a skill put on a key leaves the key it was on; keys swap and clear; each is a change', () => {
    s().load(['strike', '', 'spark', '', '', '']);
    s().put(4, 'strike');
    expect(s().slots).toEqual(['', '', 'spark', '', 'strike', '']);
    s().swap(2, 4);
    expect(s().slots).toEqual(['', '', 'strike', '', 'spark', '']);
    s().clear(2);
    expect(s().slots).toEqual(['', '', '', '', 'spark', '']);
    expect(s().edits).toBe(3);
  });

  it('cooldowns start and end per skill', () => {
    s().cool('strike', { from: 0, until: 2000 });
    s().cool('spark', { from: 0, until: 2500 });
    s().cool('strike', null);
    expect(s().cooldowns).toEqual({ spark: { from: 0, until: 2500 } });
  });

  it('a buff used again starts over; buffs end at their time, or all at once', () => {
    s().buff({ skill: 'rally', points: 1, until: 2000 });
    s().buff({ skill: 'guard', points: 2, until: 3000 });
    s().buff({ skill: 'rally', points: 3, until: 4000 });
    expect(s().buffs).toEqual([
      { skill: 'guard', points: 2, until: 3000 },
      { skill: 'rally', points: 3, until: 4000 },
    ]);
    s().endBuffs(3000);
    expect(s().buffs).toEqual([{ skill: 'rally', points: 3, until: 4000 }]);
    s().endBuffs();
    expect(s().buffs).toEqual([]);
  });

  it('a character entering the world starts the cooldowns and buffs over', () => {
    s().load(['strike', '', '', '', '', '']);
    s().cool('strike', { from: 0, until: 2000 });
    s().buff({ skill: 'rally', points: 1, until: 2000 });
    s().load(['spark']);
    expect(s()).toMatchObject({ cooldowns: {}, buffs: [] });
  });
});
