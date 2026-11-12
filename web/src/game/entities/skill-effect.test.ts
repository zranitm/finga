import { describe, expect, it } from 'vitest';

import { skillById } from '@/progression/skills';
import type { Skill, Stage } from '@/progression/skills';

import { effectOf } from './skill-effect';

const skill = (id: string): Skill => {
  const s = skillById(id);
  if (!s) throw new Error(`no skill ${id}`);
  return s;
};

describe('skill effects', () => {
  it('a melee stage draws its box in front of the feet, either way', () => {
    const strike = skill('strike');
    const heavy = strike.stages[1]!; // 80 by 56
    expect(effectOf(strike, heavy, 300, 608, 1, 56)).toEqual({
      kind: 'box',
      x: 300,
      y: 552,
      w: 80,
      h: 56,
      color: 0xf0ead6,
    });
    expect(effectOf(strike, heavy, 300, 608, -1, 56)).toMatchObject({ kind: 'box', x: 220, w: 80 });
  });

  it('a ranged stage draws its line at half height along the facing, coloured by its damage', () => {
    const spark = skill('spark');
    expect(effectOf(spark, spark.stages[0]!, 300, 608, -1, 56)).toEqual({
      kind: 'line',
      x1: 300,
      x2: 80,
      y: 580,
      color: 0xb070f0,
    });
  });

  it('an area stage draws its ring around the middle; heals and buffs have their own colours', () => {
    const area: Stage = { ...skill('spark').stages[0]!, shape: { kind: 'area', radius: 120 } };
    expect(effectOf({ ...skill('spark'), effect: 'heal' }, area, 300, 608, 1, 56)).toEqual({
      kind: 'ring',
      x: 300,
      y: 580,
      r: 120,
      color: 0x7ac74f,
    });
    expect(effectOf({ ...skill('spark'), effect: 'buff' }, area, 300, 608, 1, 56)).toMatchObject({
      color: 0xf0b23a,
    });
    expect(
      effectOf(skill('second_wind'), skill('second_wind').stages[0]!, 300, 608, 1, 56),
    ).toBeNull();
  });
});
