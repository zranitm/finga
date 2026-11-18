import { describe, expect, it } from 'vitest';

import {
  colorOf,
  bonusAt,
  buffsOn,
  learnable,
  passives,
  pointsIn,
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

  it('a skill takes its effect’s colour, a damage skill its damage type’s, and a move or passive its class’s attack’s', () => {
    const color = (id: string) => {
      const s = skillById(id)!;
      return colorOf(s, s.stages[0]!).toString(16);
    };
    expect(['heal', 'iron_body', 'taunt'].map(color)).toEqual(['4fd8ff', 'f0b23a', '6b8cff']);
    expect(['strike', 'fire_arrow'].map(color)).toEqual(['ff6a3d', 'b070f0']);
    expect(['roll_back', 'blink', 'second_wind', 'arcane_mind'].map(color)).toEqual([
      'ff6a3d',
      'b070f0',
      'ff6a3d',
      'b070f0',
    ]);
  });

  it('a class learns its own skills and those of every class above it, but a granted one its own class alone', () => {
    const beginner = ['strike', 'throw', 'spark', 'second_wind', 'lunge', 'roll_back'];
    expect(learnable('beginner').map((s) => s.id)).toEqual([...beginner, 'break_free', 'guard']);
    expect(learnable('warrior').map((s) => s.id)).toEqual([
      'slash_blast',
      'iron_body',
      'charge',
      'ground_slam',
      'war_cry',
      'taunt',
      'toughness',
      'unyielding',
      'bastion',
      ...beginner, // not Break Free or Guard: the Warrior's are Unyielding and Bastion
    ]);
    expect(learnable('nobody')).toEqual([]);
  });

  it('a granted skill counts as one point, whatever is invested', () => {
    const free = skillById('break_free')!;
    const strike = skillById('strike')!;
    expect([pointsIn(free, {}), pointsIn(free, { break_free: 4 })]).toEqual([1, 1]);
    expect([pointsIn(strike, {}), pointsIn(strike, { strike: 4 })]).toEqual([0, 4]);
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
    expect(passives('beginner', { second_wind: 9 })).toEqual({ hpRegen: 14 });
    expect(passives('beginner', { second_wind: 10 })).toEqual({ hpRegen: 20 }); // Deep Breath's base
    expect(passives('nobody', { second_wind: 10 })).toEqual({});
  });

  it('a buff adds its bonus at the points it was used with; the skills’ share adds the passives', () => {
    expect(buffsOn([{ skill: 'second_wind', points: 9 }])).toEqual({ hpRegen: 14 });
    expect(buffsOn([{ skill: 'no_such_skill', points: 9 }])).toEqual({});
    expect(
      skillsShare('beginner', { second_wind: 9 }, [{ skill: 'second_wind', points: 10 }]),
    ).toEqual({ hpRegen: 34 });
  });

  it('a cooldown cut by cooldown speed counts the server’s whole ticks, rounded up', () => {
    expect(skillCooldownTicks(2000, 0, 50)).toBe(40);
    expect(skillCooldownTicks(2000, 0.25, 50)).toBe(30);
    expect(skillCooldownTicks(2500, 0.21, 50)).toBe(40); // 1975 ms
    expect(skillCooldownTicks(2000, 0, 1000 / 12)).toBe(24); // whole ticks at 12 Hz stay whole
  });

  it('a stage costs the energy skills.yaml names, the same at every point', () => {
    expect(skill('strike').stages.map((s) => s.energy)).toEqual([2, 2, 2]);
    expect(skill('slash_blast').stages.every((s) => s.energy === 3)).toBe(true);
  });
});

describe('moves meeting mobs', () => {
  it('a break-free’s leap passes through mobs, as the server loads it; an escape’s stops at the first body', () => {
    const move = (id: string) => skillById(id)!.stages[0]!.move;
    for (const id of ['vanish', 'recoil']) {
      expect(move(id), id).toMatchObject({ kind: 'leap', through: true, stop: false });
    }
    for (const id of ['roll_back', 'smoke_bomb', 'disengage']) {
      expect(move(id), id).toMatchObject({ kind: 'leap', through: false, stop: true });
    }
  });
});
