import type Phaser from 'phaser';
import { describe, expect, it } from 'vitest';

import { skillById } from '@/progression/skills';
import type { Skill, Stage } from '@/progression/skills';

import { beatsOf } from './beats';
import { drawEffect, effectOf, slashOf } from './skill-effect';

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
      color: 0xff6a3d,
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
      color: 0x4fd8ff,
    });
    expect(effectOf({ ...skill('spark'), effect: 'buff' }, area, 300, 608, 1, 56)).toMatchObject({
      color: 0xf0b23a,
    });
    expect(
      effectOf(skill('second_wind'), skill('second_wind').stages[0]!, 300, 608, 1, 56),
    ).toBeNull();
  });

  it("a melee basic attack slashes through its box, the server's, either way", () => {
    expect(slashOf(300, 608, -1, { width: 48, height: 48 }, 'physical')).toEqual({
      kind: 'slash',
      x: 300,
      y: 584,
      rx: 48,
      ry: 24,
      facing: -1,
      color: 0xfff6d8,
    });
  });

  it('a shape holds at full strength through the strike and is gone by the cancel point', () => {
    const fades: Record<string, unknown>[] = [];
    const any: unknown = new Proxy(() => undefined, {
      get: (_t, key) => (key === 'then' ? undefined : () => any),
    });
    const scene = {
      add: { graphics: () => any, rectangle: () => any },
      tweens: { add: (c: Record<string, unknown>) => fades.push(c), killTweensOf: () => undefined },
    } as unknown as Phaser.Scene;
    const b = beatsOf(
      { kind: 'attack', ticks: 21, hit: 0, cancel: 0.85, cancelable: true },
      1000,
      50,
    );
    const slash = slashOf(300, 608, 1, { width: 48, height: 48 }, 'physical');
    drawEffect(scene, slash, b, 1000);
    expect(fades[0]?.alpha).toBe(0);
    expect(fades[0]?.delay).toBeCloseTo(850 / 3);
    expect(fades[0]?.duration).toBeCloseTo((850 * 2) / 3);
    drawEffect(scene, { kind: 'box', x: 0, y: 0, w: 10, h: 10, color: 0 }, b, 1500); // drawn after its strike: it fades from now
    expect(fades[1]).toMatchObject({ alpha: 0, delay: 0, duration: 350 });
  });
});
