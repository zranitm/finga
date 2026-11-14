import { describe, expect, it } from 'vitest';

import { skills } from '@/gamedata/skills';
import { skillById } from '@/progression/skills';
import type { Skill } from '@/progression/skills';

import { actionText, effectText, perPointText } from './skillText';

function skill(id: string): Skill {
  const s = skillById(id);
  if (!s) {
    throw new Error(`no skill ${id}`);
  }
  return s;
}
/** No cooldown speed, at the world's 20 Hz. */
const clock = { speed: 0, tickMs: 50 };
const at = (id: string, points: number) =>
  effectText(skill(id), skill(id).stages[0]!, points, clock);
const perPoint = (id: string) => perPointText(skill(id), skill(id).stages[0]!);

describe('effectText', () => {
  it('a damage skill reads as its attack, cost, reach and foes', () => {
    expect(at('strike', 8)).toBe('1.9× physical attack · 2 energy · 4.5s cooldown');
    // 5625 ms runs as the bar runs it, in whole ticks.
    expect(at('spark', 1)).toBe('1.66× magic attack · 2 energy · 5.65s cooldown · range 220');
    expect(at('slash_blast', 1)).toBe(
      '1.44× physical attack · 3 energy · 6.75s cooldown · up to 3 foes',
    );
    expect(perPoint('strike')).toBe('power +0.05×');
  });

  it('a cooldown is the one the skill bar runs, cut by cooldown speed', () => {
    const strike = skill('strike');
    // 4.5 s at 10% faster: 4050 ms, 81 ticks.
    expect(effectText(strike, strike.stages[0]!, 8, { speed: 0.1, tickMs: 50 })).toBe(
      '1.9× physical attack · 2 energy · 4.05s cooldown',
    );
    // 1% faster: 4455 ms rounds up to 90 ticks, as the bar runs it.
    expect(effectText(strike, strike.stages[0]!, 8, { speed: 0.01, tickMs: 50 })).toBe(
      '1.9× physical attack · 2 energy · 4.5s cooldown',
    );
  });

  it('a passive reads as the bonus it adds', () => {
    expect(at('second_wind', 1)).toBe('HP regen +0.6/s');
    expect(perPoint('second_wind')).toBe('HP regen +0.1/s');
  });

  it.each([
    {
      id: 'focus',
      text: 'Accuracy +22 and Crit rate +5.5% on you for 30s · 3 energy · 60s cooldown',
      each: 'Accuracy +2 · Crit rate +0.5%',
    },
    {
      id: 'haste',
      text: 'Move speed +8.5% and Evasion +11 on you for 20s · 3 energy · 40s cooldown',
      each: 'Move speed +0.5% · Evasion +1',
    },
    {
      id: 'iron_body',
      text: 'Physical defense +22 on you for 30s · 3 energy · 60s cooldown',
      each: 'Physical defense +2',
    },
  ])('$id, a buff, reads as what it gives the caster and for how long', ({ id, text, each }) => {
    expect(at(id, 1)).toBe(text);
    expect(perPoint(id)).toBe(each);
  });

  it('a heal reads as whom it reaches, as the world picks them, and how much', () => {
    expect(at('heal', 1)).toBe(
      'Heals you and the nearest hurt party members within 150, up to 3 in all, by 1.55× magic attack · 3 energy · 4.5s cooldown',
    );
    expect(perPoint('heal')).toBe('heal +0.05× magic attack');
  });

  it('a heal for one reaches the caster if hurt, else one party member', () => {
    const heal = skill('heal');
    const one = { ...heal.stages[0]!, targets: 1 };
    expect(effectText(heal, one, 1, clock)).toBe(
      'Heals you if hurt, else the nearest hurt party member within 150, by 1.55× magic attack · 3 energy · 4.5s cooldown',
    );
  });

  it('no buff or heal in the data reads as an attack on foes', () => {
    const kinds = skills.filter((s) => s.effect === 'buff' || s.effect === 'heal');
    expect(kinds.length).toBeGreaterThan(0);
    for (const s of kinds) {
      for (const st of s.stages) {
        const text = `${effectText(s, st, 1, clock)} | ${perPointText(s, st)}`;
        expect(text, s.id).not.toMatch(/physical attack|power \+|radius|foes/);
      }
    }
  });
});

describe('actionText', () => {
  it('reads an active stage’s length, and says a basic attack can cut short a cancelable one', () => {
    expect(actionText(skill('strike').stages[0]!)).toBe(
      'Takes 1.5s. A basic attack can cut it short.',
    );
    expect(actionText({ ...skill('heal').stages[0]!, length_ms: 700 })).toBe('Takes 0.7s.');
  });
});
