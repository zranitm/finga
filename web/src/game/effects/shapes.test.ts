import type Phaser from 'phaser';
import { describe, expect, it } from 'vitest';

import { skillById } from '@/progression/skills';
import type { Skill, Stage } from '@/progression/skills';

import { beatsOf } from '../entities/beats';

import { colorOf, drawShape, shapeAt, slashAt } from './shapes';

const at = (facing: 1 | -1) => ({ x: 300, y: 608, facing });
const shapeOf = (skill: Skill, stage: Stage, facing: 1 | -1) =>
  shapeAt(stage.shape, at(facing), colorOf(skill, stage));
const skill = (id: string): Skill => {
  const s = skillById(id);
  if (!s) throw new Error(`no skill ${id}`);
  return s;
};

describe('drawn shapes', () => {
  it('a melee stage draws its box in front of the feet, either way', () => {
    const strike = skill('strike');
    const heavy = strike.stages[1]!; // 80 by 56
    expect(shapeOf(strike, heavy, 1)).toEqual({
      kind: 'box',
      x: 300,
      y: 552,
      w: 80,
      h: 56,
      color: 0xff6a3d,
    });
    expect(shapeOf(strike, heavy, -1)).toMatchObject({ kind: 'box', x: 220, w: 80 });
  });

  it('a ranged stage draws its line at half height along the facing, coloured by its damage', () => {
    const spark = skill('spark');
    expect(shapeOf(spark, spark.stages[0]!, -1)).toEqual({
      kind: 'line',
      x1: 300,
      x2: 80,
      y: 580,
      color: 0xb070f0,
    });
  });

  it('an area stage draws its ring around the middle; heals and buffs have their own colours', () => {
    const area: Stage = { ...skill('spark').stages[0]!, shape: { kind: 'area', radius: 120 } };
    expect(shapeOf({ ...skill('spark'), effect: 'heal' }, area, 1)).toEqual({
      kind: 'ring',
      x: 300,
      y: 580,
      r: 120,
      color: 0x4fd8ff,
    });
    expect(shapeOf({ ...skill('spark'), effect: 'buff' }, area, 1)).toMatchObject({
      color: 0xf0b23a,
    });
    expect(shapeOf(skill('second_wind'), skill('second_wind').stages[0]!, 1)).toBeNull();
  });

  it("a melee basic attack slashes through its box, the server's, either way", () => {
    expect(slashAt(at(-1), { width: 48, height: 48 }, 0xfff6d8)).toEqual({
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
    drawShape(scene, slashAt(at(1), { width: 48, height: 48 }, 0xfff6d8), b, 1000);
    expect(fades[0]?.alpha).toBe(0);
    expect(fades[0]?.delay).toBeCloseTo(850 / 3);
    expect(fades[0]?.duration).toBeCloseTo((850 * 2) / 3);
    drawShape(scene, { kind: 'box', x: 0, y: 0, w: 10, h: 10, color: 0 }, b, 1500); // drawn after its strike: it fades from now
    expect(fades[1]).toMatchObject({ alpha: 0, delay: 0, duration: 350 });
  });
});
