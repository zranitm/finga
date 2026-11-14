import { describe, expect, it } from 'vitest';

import { healOf, missing, pickPotion, restores } from './potions';

const item = (template: string, slot: number) => ({ itemId: `i-${slot}`, template });

const vitals = (hp: number) => ({ hp, maxHp: 200 });

describe('potions', () => {
  it('knows a potion by its Use tab and its heal, as the server does', () => {
    expect(healOf('red_potion')).toBe(50);
    expect(healOf('white_potion')).toBe(300);
    expect(healOf('slime_gel')).toBeUndefined();
    expect(healOf('wooden_sword')).toBeUndefined();
    expect(healOf('elixir_of_ages')).toBeUndefined();
  });

  it('picks the first stack of equals, and nothing from a tab without a potion', () => {
    const tab = [null, item('slime_gel', 1), item('red_potion', 2), item('red_potion', 3)];
    expect(pickPotion(20, tab)?.itemId).toBe('i-2');
    expect(pickPotion(500, tab)?.itemId).toBe('i-2');
    expect(pickPotion(10, [item('slime_gel', 0)])).toBeUndefined();
    expect(pickPotion(10, [])).toBeUndefined();
  });

  it.each([
    { need: 40, want: 'red_potion' },
    { need: 50, want: 'red_potion' },
    { need: 51, want: 'orange_potion' },
    { need: 200, want: 'white_potion' },
    { need: 900, want: 'white_potion' },
  ])(
    'with $need HP missing, picks the $want: the smallest that fills it, else the largest',
    ({ need, want }) => {
      const tab = ['white_potion', 'red_potion', 'orange_potion'].map(item);
      expect(pickPotion(need, tab)?.template).toBe(want);
    },
  );

  it('restores no more than is missing, and nothing at full HP', () => {
    expect(restores('red_potion', vitals(20))).toBe(50);
    expect(restores('red_potion', vitals(180))).toBe(20);
    expect(restores('red_potion', vitals(200))).toBe(0);
    expect(missing(vitals(200))).toBe(0);
    expect(missing(vitals(210))).toBe(0);
  });
});
