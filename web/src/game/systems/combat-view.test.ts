import { describe, expect, it, vi } from 'vitest';

import {
  AttackGate,
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  NUMBER_LIFE_MS,
  classCooldownMs,
  numberFor,
  parseRef,
} from './combat-view';
import type { MobWire } from './combat-view';

const slime = (id: number, x: number, hp = 30): MobWire => ({
  id,
  type: 'green_slime',
  x,
  y: 608,
  facing: -1,
  hp,
  max_hp: 30,
});

describe('parseRef', () => {
  it.each([
    { ref: 'p:c-ayla', want: { kind: 'player', id: 'c-ayla' } },
    { ref: 'm:7', want: { kind: 'mob', id: 7 } },
    { ref: 'm:x', want: undefined },
    { ref: 'p:', want: undefined },
    { ref: 'q:1', want: undefined },
  ])('$ref', ({ ref, want }) => {
    expect(parseRef(ref)).toEqual(want);
  });
});

describe('MobTracker', () => {
  it('a Snapshot sets the mobs and drops the rest; Deltas update and remove', () => {
    const m = new MobTracker();
    m.applySnapshot(1000, [slime(1, 100), slime(2, 200)]);
    m.applyDelta(1050, [slime(1, 110)], [2]);
    expect(m.render(1025).shown.get(1)?.x).toBe(105);
    expect(m.render(1049).shown.has(2)).toBe(true);
    const { shown, gone } = m.render(1050);
    expect(gone).toEqual([2]);
    expect(shown.get(1)).toEqual({
      type: 'green_slime',
      x: 110,
      y: 608,
      facing: -1,
      hp: 30,
      max_hp: 30,
    });
    m.applySnapshot(1100, [slime(3, 300)]);
    expect(m.size).toBe(1);
    expect(m.render(1100).shown.has(3)).toBe(true);
  });

  it('a Damage for a mob updates its HP at once, and the next state confirms or corrects it', () => {
    const m = new MobTracker();
    m.applySnapshot(1000, [slime(1, 100)]);
    m.damage(1, 12);
    expect(m.hpOf(1)).toBe(18);
    expect(m.render(1000).shown.get(1)?.hp).toBe(18);
    m.damage(1, 50);
    expect(m.hpOf(1)).toBe(0);
    m.applyDelta(1050, [slime(1, 100, 18)], []);
    expect(m.hpOf(1)).toBe(18);
    m.damage(99, 5); // unknown: ignored
    m.clear();
    expect(m.size).toBe(0);
  });
});

describe('numberFor and DamageNumbers', () => {
  it.each([
    {
      name: 'a hit',
      d: { source: 'p:a', target: 'm:1', amount: 7 },
      me: false,
      want: { text: '7', kind: 'hit' },
    },
    {
      name: 'a crit',
      d: { source: 'p:a', target: 'm:1', amount: 14, crit: true },
      me: false,
      want: { text: '14!', kind: 'crit' },
    },
    {
      name: 'a miss',
      d: { source: 'p:a', target: 'm:1', amount: 0, miss: true },
      me: false,
      want: { text: 'MISS', kind: 'miss' },
    },
    {
      name: 'a hit on me',
      d: { source: 'm:1', target: 'p:a', amount: 5 },
      me: true,
      want: { text: '-5', kind: 'taken' },
    },
  ])('$name', ({ d, me, want }) => {
    expect(numberFor(d, me)).toEqual(want);
  });

  it('queues one number per spawn, rises and fades, and expires', () => {
    const n = new DamageNumbers();
    n.spawn(100, 600, '7', 'hit', 1000);
    expect(n.size).toBe(1);
    const start = n.update(1000)[0]!;
    expect(start.dy).toBe(0);
    expect(start.alpha).toBe(1);
    const mid = n.update(1000 + NUMBER_LIFE_MS * 0.75)[0]!;
    expect(mid.dy).toBeCloseTo(-30);
    expect(mid.alpha).toBeCloseTo(0.5);
    expect(n.update(1000 + NUMBER_LIFE_MS)).toEqual([]);
    expect(n.size).toBe(0);
    n.spawn(1, 1, 'MISS', 'miss', 2000);
    n.clear();
    expect(n.size).toBe(0);
  });
});

describe('AttackGate', () => {
  it.each([
    { cls: 'warrior', ms: 600 },
    { cls: 'bowman', ms: 800 },
    { cls: 'unknown', ms: 600 },
  ])('$cls cools down for $ms ms', ({ cls, ms }) => {
    expect(classCooldownMs(cls)).toBe(ms);
  });

  it('the attack key during cooldown sends nothing; after cooldown sends exactly one Attack', () => {
    const send = vi.fn(() => true);
    const g = new AttackGate(600, send);
    expect(g.tryAttack(1000)).toBe(true);
    expect(g.tryAttack(1100)).toBe(false);
    expect(g.tryAttack(1599)).toBe(false);
    expect(send).toHaveBeenCalledTimes(1);
    expect(g.tryAttack(1600)).toBe(true);
    expect(send).toHaveBeenCalledTimes(2);
    g.reset();
    expect(g.tryAttack(1601)).toBe(true);
  });

  it('a refused send (socket closed) still starts the cooldown', () => {
    const g = new AttackGate(600, () => false);
    expect(g.tryAttack(0)).toBe(false);
    expect(g.tryAttack(1)).toBe(false);
  });
});

describe('InvulnFlash', () => {
  it('a state with invulnerable: true starts the flash, and one without it ends it', () => {
    const f = new InvulnFlash();
    expect(f.alpha(0)).toBe(1);
    f.set(true, 1000);
    expect(f.active).toBe(true);
    expect(f.alpha(1000)).toBe(0.4);
    expect(f.alpha(1150)).toBe(1);
    expect(f.alpha(1250)).toBe(0.4);
    f.set(true, 1300); // still on: the phase is kept
    expect(f.alpha(1350)).toBe(1);
    f.set(false, 1400);
    expect(f.active).toBe(false);
    expect(f.alpha(1400)).toBe(1);
  });
});
