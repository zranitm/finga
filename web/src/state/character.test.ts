import { beforeEach, describe, expect, it } from 'vitest';

import { expCurve } from '@/gamedata/exp-curve';
import type { CharacterView } from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { character, expToNextLevel } from './character';

const ayla: CharacterView = {
  id: 'c1',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  max_hp: 204,
  mp: 28,
  max_mp: 28,
};

const welcome = {
  token: 't',
  character: ayla,
  inventory: [],
  map_id: 'town',
  tick: 1,
  server_time: 0,
};

describe('character store', () => {
  beforeEach(() => {
    resetStores();
  });

  it.each([
    { level: 1, want: 20 },
    { level: 2, want: 65 },
    { level: 29, want: 6125 },
    { level: 30, want: null },
    { level: 31, want: null },
  ])(
    'level $level needs $want EXP for the next level, from the generated curve',
    ({ level, want }) => {
      expect(expToNextLevel(level)).toBe(want);
      if (want !== null) {
        expect(expCurve.exp_to_next.find((e) => e.level === level)?.exp).toBe(want);
      }
    },
  );

  it('Welcome sets the character and the EXP to the next level', () => {
    character.getState().applyWelcome(welcome);
    expect(character.getState().character).toEqual(ayla);
    expect(character.getState().expToNext).toBe(20);
  });

  it('ExpGained sets exp to the payload total, up or down (the death penalty)', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyExpGained({ amount: 5, exp: 15 });
    expect(character.getState().character?.exp).toBe(15);
    character.getState().applyExpGained({ amount: -1, exp: 14 });
    expect(character.getState().character?.exp).toBe(14);
  });

  it('LevelUp replaces the character and moves the EXP target', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyLevelUp({
      character: {
        ...ayla,
        level: 2,
        exp: 3,
        stats: { ...ayla.stats, str: 15, vit: 12 },
        hp: 252,
        max_hp: 252,
      },
    });
    expect(character.getState().character).toMatchObject({ level: 2, exp: 3, hp: 252 });
    expect(character.getState().expToNext).toBe(65);
  });

  it('vitals change only when they differ, and clear empties the store', () => {
    character.getState().applyWelcome(welcome);
    const before = character.getState().character;
    character.getState().setVitals(204, 28);
    expect(character.getState().character).toBe(before);
    character.getState().setVitals(100, 28);
    expect(character.getState().character?.hp).toBe(100);
    character.getState().clear();
    expect(character.getState().character).toBeNull();
    expect(character.getState().expToNext).toBeNull();
    character.getState().applyExpGained({ amount: 1, exp: 1 }); // no character: nothing to do
    expect(character.getState().character).toBeNull();
  });
});
