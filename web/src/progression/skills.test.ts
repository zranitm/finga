import { describe, expect, it } from 'vitest';

import {
  bonusAt,
  buffsOn,
  learnable,
  mpAt,
  passives,
  skillById,
  skillCooldownTicks,
  skillsShare,
  stageAt,
} from './skills';

const skill = (id: string) => {
  const s = skillById(id);
  if (!s) throw new Error(`no skill ${id}`);
  return s;
};

describe('skills', () => {
  it('a stage holds from its total until the next', () => {
    const strike = skill('strike');
    expect([0, 9, 10, 24, 25, 99].map((p) => strike.stages[stageAt(strike, p)]!.name)).toEqual([
      'Strike',
      'Strike',
      'Heavy Strike',
      'Heavy Strike',
      'Crushing Blow',
      'Crushing Blow',
    ]);
  });

  it('a class learns its own skills and those of every class above it', () => {
    const beginner = ['strike', 'throw', 'spark', 'second_wind'];
    expect(learnable('beginner').map((s) => s.id)).toEqual(beginner);
    expect(learnable('warrior').map((s) => s.id)).toEqual([
      'slash_blast',
      'iron_body',
      ...beginner,
    ]);
    expect(learnable('nobody')).toEqual([]);
  });

  it('a bonus rounds to whole item units, and names stats as the sheet does', () => {
    expect(
      bonusAt({ hp_regen: { base: 5, per_point: 0.5 }, str: { base: 1, per_point: 0 } }, 3),
    ).toEqual({
      hpRegen: 7,
      str: 1,
    });
    expect(bonusAt({ no_such_stat: { base: 5, per_point: 1 } }, 3)).toEqual({});
  });

  it('only passives with a point add, each at its stage', () => {
    expect(passives('beginner', {})).toEqual({});
    expect(passives('beginner', { strike: 20, second_wind: 0 })).toEqual({});
    expect(passives('beginner', { second_wind: 9 })).toEqual({ hpRegen: 14, mpRegen: 14 });
    expect(passives('beginner', { second_wind: 10 })).toEqual({ hpRegen: 20, mpRegen: 20 }); // Deep Breath's base
    expect(passives('nobody', { second_wind: 10 })).toEqual({});
  });

  it('a buff adds its bonus at the points it was used with; the skills’ share adds the passives', () => {
    expect(buffsOn([{ skill: 'second_wind', points: 9 }])).toEqual({ hpRegen: 14, mpRegen: 14 });
    expect(buffsOn([{ skill: 'no_such_skill', points: 9 }])).toEqual({});
    expect(
      skillsShare('beginner', { second_wind: 9 }, [{ skill: 'second_wind', points: 10 }]),
    ).toEqual({
      hpRegen: 34,
      mpRegen: 34,
    });
  });

  it('a cooldown cut by cooldown speed counts the server’s whole ticks, rounded up', () => {
    expect(skillCooldownTicks(2000, 0, 50)).toBe(40);
    expect(skillCooldownTicks(2000, 0.25, 50)).toBe(30);
    expect(skillCooldownTicks(2500, 0.21, 50)).toBe(40); // 1975 ms
    expect(skillCooldownTicks(2000, 0, 1000 / 12)).toBe(24); // whole ticks at 12 Hz stay whole
  });

  it('a cost is a whole number of MP', () => {
    const strike = skill('strike');
    expect([0, 2, 3, 8].map((p) => mpAt(strike.stages[0]!, p))).toEqual([4, 4, 5, 6]);
  });
});
