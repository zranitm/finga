import { describe, expect, it } from 'vitest';

import { conditionText, followUpOf, keyable } from './chains';

describe('chains', () => {
  it('a skill stands on a key of its own if it roots a chain of its class’s or is in none, as the world allows', () => {
    expect(keyable('warrior', 'strike')).toBe(true); // a root
    expect(keyable('warrior', 'slash_blast')).toBe(true); // in no chain
    expect(keyable('warrior', 'charge')).toBe(false);
    expect(keyable('warrior', 'lunge')).toBe(false); // Roll Back's counter
    expect(keyable('thief', 'lunge')).toBe(true); // the Thief's chains leave it out
    expect(keyable('magician', 'fire_arrow')).toBe(true); // Blink's counter, but a root too
    expect(keyable('beginner', 'spark')).toBe(true);
    expect(keyable('beginner', 'lunge')).toBe(false);
  });

  it('says a condition as the window and the tooltip do', () => {
    const say = (cls: string, root: string, skill: string, name: string) =>
      conditionText(followUpOf(cls, root, skill)!.when, name);
    expect(say('thief', 'double_stab', 'flurry', 'Double Stab')).toBe('right after Double Stab');
    expect(say('warrior', 'strike', 'ground_slam', 'Strike')).toBe(
      'while the target is stunned or frozen',
    );
    expect(say('bowman', 'power_shot', 'arrow_rain', 'Power Shot')).toBe(
      'while the target is stunned, frozen, pinned or knocked down',
    );
    expect(say('warrior', 'strike', 'charge', 'Strike')).toBe('while the target is over 3 m away');
    expect(say('bowman', 'disengage', 'power_shot', 'Disengage')).toBe('right after a dodge');
    expect(followUpOf('warrior', 'strike', 'flurry')).toBeUndefined();
  });
});
