import { describe, expect, it } from 'vitest';

import { fullFor, healOf, pickPotion, restores } from './potions';

const item = (template: string, slot: number) => ({ itemId: `i-${slot}`, template });

const vitals = (hp: number, mp: number) => ({ hp, maxHp: 200, mp, maxMp: 100 });

describe('potions', () => {
  it('knows a potion by its Use tab and its heal, as the server does', () => {
    expect(healOf('red_potion')).toEqual({ hp: 50, mp: 0 });
    expect(healOf('blue_potion')).toEqual({ hp: 0, mp: 30 });
    expect(healOf('slime_gel')).toBeUndefined();
    expect(healOf('wooden_sword')).toBeUndefined();
    expect(healOf('elixir_of_ages')).toBeUndefined();
  });

  it('picks a potion of the vital asked for, the first stack of equals', () => {
    const tab = [null, item('blue_potion', 1), item('red_potion', 2), item('red_potion', 3)];
    expect(pickPotion('hp', 20, tab)?.itemId).toBe('i-2');
    expect(pickPotion('hp', 500, tab)?.itemId).toBe('i-2');
    expect(pickPotion('mp', 10, tab)?.itemId).toBe('i-1');
    expect(pickPotion('mp', 10, [item('red_potion', 0)])).toBeUndefined();
    expect(pickPotion('hp', 10, [])).toBeUndefined();
  });

  it('restores no more than is missing', () => {
    expect(restores('red_potion', vitals(20, 100))).toEqual({ hp: 50, mp: 0 });
    expect(restores('red_potion', vitals(180, 0))).toEqual({ hp: 20, mp: 0 });
    expect(restores('blue_potion', vitals(0, 90))).toEqual({ hp: 0, mp: 10 });
  });

  it('names the full vital a potion would be refused for', () => {
    expect(fullFor('red_potion', vitals(200, 0))).toBe('hp');
    expect(fullFor('blue_potion', vitals(0, 100))).toBe('mp');
    expect(fullFor('red_potion', vitals(199, 100))).toBeNull();
    expect(fullFor('slime_gel', vitals(200, 100))).toBeNull();
  });
});
