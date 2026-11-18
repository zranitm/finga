import { describe, expect, it } from 'vitest';

import { SKILL_KEYS } from '@/state/skill-bar';

import { hiddenCooldowns } from './hiddenCooldowns';

const keys = (...skills: string[]) => Array.from({ length: SKILL_KEYS }, (_, k) => skills[k] ?? '');
const cooling = (until: number) => ({ from: 0, until });
const shown = (list: ReturnType<typeof hiddenCooldowns>) =>
  list.map((c) => ({ skill: c.skill, on: c.on.map((o) => o.root), until: c.cooldown.until }));

describe('hidden cooldowns (frame-icons.md section 3)', () => {
  it('shows a follow-up of a key’s chain while it cools and no key shows it, and drops it as its cooldown ends', () => {
    const bar = keys('strike', 'roll_back');
    const got = hiddenCooldowns(
      'warrior',
      bar,
      bar,
      { ground_slam: cooling(7000) },
      { ground_slam: 1 },
    );
    expect(shown(got)).toEqual([{ skill: 'ground_slam', on: ['strike'], until: 7000 }]);
    expect(got[0]!.on[0]!.when.controlled).toEqual(['stun', 'freeze']);
    expect(hiddenCooldowns('warrior', bar, bar, {}, { ground_slam: 1 })).toEqual([]);
  });

  it('shows none while a key shows it: its root’s key offering it now, or a key of its own', () => {
    const bar = keys('strike');
    const offered = keys('ground_slam');
    expect(
      hiddenCooldowns('warrior', bar, offered, { ground_slam: cooling(7000) }, { ground_slam: 1 }),
    ).toEqual([]);
    const own = keys('power_shot', 'disengage');
    expect(
      hiddenCooldowns('bowman', own, own, { power_shot: cooling(5000) }, { power_shot: 1 }),
    ).toEqual([]);
    const rootOnly = keys('disengage');
    expect(
      shown(
        hiddenCooldowns(
          'bowman',
          rootOnly,
          rootOnly,
          { power_shot: cooling(5000) },
          { power_shot: 1 },
        ),
      ),
    ).toEqual([{ skill: 'power_shot', on: ['disengage'], until: 5000 }]);
  });

  it('shows none for a skill with no points, or one whose chain’s root is on no key', () => {
    const bar = keys('strike');
    expect(hiddenCooldowns('warrior', bar, bar, { ground_slam: cooling(7000) }, {})).toEqual([]);
    const other = keys('war_cry');
    expect(
      hiddenCooldowns('warrior', other, other, { ground_slam: cooling(7000) }, { ground_slam: 1 }),
    ).toEqual([]);
  });

  it('shows a break-free through its cooldown, granted with no points, and not while its key shows it cooling', () => {
    const bar = keys('roll_back');
    expect(
      shown(hiddenCooldowns('beginner', bar, bar, { break_free: cooling(32000) }, {})),
    ).toEqual([{ skill: 'break_free', on: ['roll_back'], until: 32000 }]);
    expect(
      hiddenCooldowns('beginner', bar, keys('break_free'), { break_free: cooling(32000) }, {}),
    ).toEqual([]);
  });

  it('shows a skill two keys’ chains share once, naming both, soonest ready first', () => {
    const bar = keys('strike', 'roll_back');
    const got = hiddenCooldowns(
      'beginner',
      bar,
      bar,
      { lunge: cooling(9000), break_free: cooling(4000) },
      { lunge: 1 },
    );
    expect(shown(got)).toEqual([
      { skill: 'break_free', on: ['roll_back'], until: 4000 },
      { skill: 'lunge', on: ['strike', 'roll_back'], until: 9000 },
    ]);
  });
});
