import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';

import {
  blockOf,
  colorOf,
  bonusAt,
  buffsOn,
  learnable,
  passives,
  pointsIn,
  skillById,
  skillCooldownTicks,
  skillsShare,
  specAt,
  stageAt,
  traitOpen,
  usedStage,
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

  it('a skill takes its effect’s colour, a damage skill its damage type’s, a block its class’s own, and a move or passive its class’s attack’s', () => {
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
    const blocks = classes.map((c) => blockOf(c.id)!.id);
    expect(blocks.map(color)).toEqual(['ff9a3c', '4f86ff', 'a66bff', '46d160', 'ff6ad5']); // Guard, Bastion, Parry, Deflect, Ward
    expect(new Set(blocks.map(color)).size).toBe(classes.length); // one each, from the live pass
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

  // skill-traits.md section 2: the extra opens at the last evolution, or the first point for a skill with one stage, and unknown or locked falls back to the default, as the world's Spec does.
  it('a trait opens at the last evolution, and a skill is used at its extra’s stage only once it’s open', () => {
    const strike = skill('strike');
    expect([24, 25].map((p) => traitOpen(strike, p))).toEqual([false, true]);
    expect([0, 1].map((p) => traitOpen(skill('iron_body'), p))).toEqual([false, true]);
    expect(specAt(strike, 25, 'concussion')).toBe(strike.traits[1]!.stage);
    expect(specAt(strike, 25, 'concussion').cooldown_ms).toBe(7000);
    expect(specAt(strike, 24, 'concussion')).toBe(strike.stages[1]);
    for (const trait of [undefined, '', 'swift', 'no_such_trait']) {
      expect(specAt(strike, 25, trait)).toBe(strike.stages[2]);
    }
    const breath = skill('second_wind'); // a passive has none
    expect(breath.traits).toEqual([]);
    expect(specAt(breath, 10, 'concussion')).toBe(breath.stages[1]);
  });

  it('a SkillUsed draws at its extra trait’s stage, else at its stage', () => {
    const strike = skill('strike');
    expect(usedStage(strike, 2, 'concussion')).toBe(strike.traits[1]!.stage);
    expect(usedStage(strike, 2, '')).toBe(strike.stages[2]);
    expect(usedStage(strike, 1, '')).toBe(strike.stages[1]);
    expect(usedStage(strike, 3, '')).toBeUndefined();
  });

  it('only passives with a point add, each at its stage', () => {
    expect(passives('beginner', {})).toEqual({});
    expect(passives('beginner', { strike: 20, second_wind: 0 })).toEqual({});
    expect(passives('beginner', { second_wind: 9 })).toEqual({ hpRegen: 14 });
    expect(passives('beginner', { second_wind: 10 })).toEqual({ hpRegen: 20 }); // Deep Breath's base
    expect(passives('nobody', { second_wind: 10 })).toEqual({});
  });

  it('a buff adds its bonus at the points it was used with; the skills’ share adds the passives', () => {
    expect(buffsOn([{ skill: 'second_wind', points: 9, trait: '' }])).toEqual({ hpRegen: 14 });
    expect(buffsOn([{ skill: 'no_such_skill', points: 9, trait: '' }])).toEqual({});
    expect(buffsOn([{ skill: 'war_cry', points: 10, trait: '' }])).toEqual({
      physicalAttack: 18,
      magicAttack: 18,
    });
    expect(buffsOn([{ skill: 'war_cry', points: 10, trait: 'fury' }])).toEqual({
      physicalAttack: 36,
      magicAttack: 36,
    }); // twice the attack
    expect(buffsOn([{ skill: 'war_cry', points: 9, trait: 'fury' }])).toEqual({
      physicalAttack: 17,
      magicAttack: 17,
    }); // not open below Battle Roar
    expect(
      skillsShare('beginner', { second_wind: 9 }, [
        { skill: 'second_wind', points: 10, trait: '' },
      ]),
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
