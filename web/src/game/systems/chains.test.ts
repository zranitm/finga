import { describe, expect, it } from 'vitest';

import { ChainMemory, conditionText, followUpOf, holds, keyable, liveSkill } from './chains';
import type { ChainNow } from './chains';

/** A Warrior with a point in every follow-up of the shipped chains, nothing cooling, no hit, dodge or lock, at input 100 of 50 ms ticks. */
const at = (n: Partial<ChainNow> = {}): ChainNow => ({
  seq: 100,
  tickMs: 50,
  now: 1000,
  invested: {
    lunge: 1,
    charge: 1,
    ground_slam: 1,
    iron_body: 1,
    flurry: 1,
    shadow_step: 1,
    power_shot: 1,
    meteor: 1,
    fire_arrow: 1,
  },
  cooldowns: {},
  hits: {},
  dodged: null,
  lock: null,
  ...n,
});
const lock = (far: number, statuses: { id: string; ends: number }[] = []) => ({
  far,
  statuses,
  tick: 500,
});

describe('chains', () => {
  it('a far lock turns the key into the approach, past the distance and not at it', () => {
    expect(liveSkill('warrior', 'strike', at({ lock: lock(3.01) }))).toBe('charge');
    expect(liveSkill('warrior', 'strike', at({ lock: lock(3) }))).toBe('strike');
    expect(liveSkill('warrior', 'strike', at())).toBe('strike'); // no lock
    expect(liveSkill('beginner', 'strike', at({ lock: lock(5) }))).toBe('lunge'); // the Beginner's chain
  });

  it('a control the chain names, still running at the newest frame, turns the key into the finisher', () => {
    const stunned = lock(1, [{ id: 'stun', ends: 501 }]);
    expect(liveSkill('warrior', 'strike', at({ lock: stunned }))).toBe('ground_slam');
    expect(liveSkill('warrior', 'strike', at({ lock: lock(1, [{ id: 'stun', ends: 500 }]) }))).toBe(
      'strike',
    ); // ended on the newest frame's tick
    expect(liveSkill('warrior', 'strike', at({ lock: lock(1, [{ id: 'pin', ends: 900 }]) }))).toBe(
      'strike',
    ); // Ground Slam waits for a stun or freeze, not a pin
    expect(
      liveSkill('magician', 'fire_arrow', at({ lock: lock(1, [{ id: 'pin', ends: 900 }]) })),
    ).toBe('meteor');
  });

  it('the first follow-up listed whose condition holds wins: a stunned lock far off is Ground Slam', () => {
    expect(liveSkill('warrior', 'strike', at({ lock: lock(8, [{ id: 'stun', ends: 900 }]) }))).toBe(
      'ground_slam',
    );
  });

  it('a combo is open from its root’s hit, counted in inputs, to the window’s end', () => {
    const hits = { double_stab: 100 }; // pressed during input 100, its hit at share 0
    expect(liveSkill('thief', 'double_stab', at({ seq: 99, hits }))).toBe('double_stab'); // before the hit
    expect(liveSkill('thief', 'double_stab', at({ seq: 100, hits }))).toBe('flurry');
    expect(liveSkill('thief', 'double_stab', at({ seq: 140, hits }))).toBe('flurry'); // 2 s: 40 inputs
    expect(liveSkill('thief', 'double_stab', at({ seq: 141, hits }))).toBe('double_stab');
  });

  it('a counter is open from the dodge’s frame to the window’s end, counted in inputs', () => {
    expect(liveSkill('bowman', 'disengage', at({ seq: 130, dodged: 100 }))).toBe('power_shot'); // 1.5 s: 30 inputs
    expect(liveSkill('bowman', 'disengage', at({ seq: 131, dodged: 100 }))).toBe('disengage');
    expect(liveSkill('bowman', 'disengage', at({ dodged: null }))).toBe('disengage');
  });

  it('a follow-up with no point, or cooling down, leaves the key its root', () => {
    const far = lock(9);
    expect(liveSkill('warrior', 'strike', at({ lock: far, invested: {} }))).toBe('strike');
    const cooling = { charge: { from: 0, until: 1001 } };
    expect(liveSkill('warrior', 'strike', at({ lock: far, cooldowns: cooling }))).toBe('strike');
    const cooled = { charge: { from: 0, until: 1000 } };
    expect(liveSkill('warrior', 'strike', at({ lock: far, cooldowns: cooled }))).toBe('charge');
    // With Ground Slam cooling on a stunned lock far off, the next listed takes over.
    const slamCooling = { ground_slam: { from: 0, until: 2000 } };
    expect(
      liveSkill(
        'warrior',
        'strike',
        at({ lock: lock(9, [{ id: 'stun', ends: 900 }]), cooldowns: slamCooling }),
      ),
    ).toBe('charge');
  });

  it('a key with no chain, or a class with none from that root, is its own skill', () => {
    expect(liveSkill('warrior', 'slash_blast', at({ lock: lock(9) }))).toBe('slash_blast');
    expect(liveSkill('warrior', 'double_stab', at({ hits: { double_stab: 100 } }))).toBe(
      'double_stab',
    ); // the Thief's chain, not the Warrior's
  });

  it('holds reads each condition alone', () => {
    const after = { after_ms: 3000, controlled: null, far: null, dodged_ms: null };
    expect(holds('war_cry', after, at({ hits: { war_cry: 50 }, seq: 110 }))).toBe(true);
    expect(holds('war_cry', after, at({ hits: { war_cry: 50 }, seq: 111 }))).toBe(false);
  });

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

describe('ChainMemory', () => {
  it('keeps each skill’s latest hit in inputs, an undo giving back the one before unless a later use stands', () => {
    const m = new ChainMemory();
    m.used('strike', 10, 4);
    const undo = m.used('strike', 30, 4);
    expect(m.hits).toEqual({ strike: 34 });
    undo();
    expect(m.hits).toEqual({ strike: 14 });
    const refused = m.used('strike', 50, 4);
    m.used('strike', 60, 4);
    refused();
    expect(m.hits).toEqual({ strike: 64 });
    const first = new ChainMemory();
    first.used('spark', 5, 0)();
    expect(first.hits).toEqual({});
  });

  it('keeps the latest dodge, and a new map forgets both', () => {
    const m = new ChainMemory();
    m.dodge(40);
    m.dodge(55);
    m.used('strike', 10, 4);
    expect([m.dodged, m.hits]).toEqual([55, { strike: 14 }]);
    m.clear();
    expect([m.dodged, m.hits]).toEqual([null, {}]);
  });
});
