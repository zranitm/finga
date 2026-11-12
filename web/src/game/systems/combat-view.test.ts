import { create } from '@bufbuild/protobuf';
import { describe, expect, it, vi } from 'vitest';

import { MobViewSchema, StatSheetSchema } from '@/protocol/messages';

import {
  AttackGate,
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  NUMBER_LIFE_MS,
  attackCooldownTicks,
  numberFor,
  parseRef,
} from './combat-view';

const slime = (id: number, x: number, hp = 30) =>
  create(MobViewSchema, { id, type: 'green_slime', x, y: 608, facing: -1, hp, maxHp: 30 });

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
    const m = new MobTracker(50);
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
      maxHp: 30,
    });
    m.applySnapshot(1100, [slime(3, 300)]);
    expect(m.size).toBe(1);
    expect(m.render(1100).shown.has(3)).toBe(true);
  });

  it("shows a mob's HP as the server last sent it, and a killed mob at 0 until it drains", () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 100), slime(2, 200)]);
    m.applyDelta(1050, [slime(1, 100, 18)], [2, 99]); // 99: unknown, ignored
    const { shown } = m.render(1025);
    expect(shown.get(1)?.hp).toBe(18);
    expect(shown.get(2)?.hp).toBe(0);
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
  it('the attack key during cooldown sends nothing; after cooldown sends exactly one Attack', () => {
    const send = vi.fn(() => true);
    const g = new AttackGate(12, 50, send);
    expect(g.tryAttack(1000)).toBe(true);
    expect(g.tryAttack(1100)).toBe(false);
    expect(g.tryAttack(1599)).toBe(false);
    expect(send).toHaveBeenCalledTimes(1);
    expect(g.tryAttack(1600)).toBe(true);
    expect(send).toHaveBeenCalledTimes(2);
    g.reset();
    expect(g.tryAttack(1601)).toBe(true);
  });

  it('a skill takes the next turn, counted from the last turn’s end, and gives it back unless another turn was taken since', () => {
    const g = new AttackGate(12, 50, () => true);
    expect(g.readyIn(1000)).toBe(0);
    g.tryAttack(1000);
    expect(g.readyIn(1200)).toBe(400);
    const undo = g.take(1500); // early, as a skill waiting for the turn never is: the turn after still counts from 1600
    expect(g.readyIn(1500)).toBe(700);
    undo();
    expect(g.readyIn(1500)).toBe(100);
    const late = g.take(1600);
    g.tryAttack(2200);
    late(); // an attack took a turn since
    expect(g.readyIn(2200)).toBe(600);
  });

  it('waits the ticks the sheet says, and a new count from attack speed applies from the next attack', () => {
    const g = new AttackGate(8, 1000 / 12, () => true);
    expect(g.tryAttack(1000)).toBe(true);
    g.setCooldownTicks(6);
    expect(g.tryAttack(1000 + 8000 / 12 - 1)).toBe(false);
    expect(g.tryAttack(1000 + 8000 / 12)).toBe(true);
    expect(g.tryAttack(1000 + 8000 / 12 + 500 - 1)).toBe(false);
    expect(g.tryAttack(1000 + 8000 / 12 + 500)).toBe(true);
  });

  it.each([
    { name: 'the sheet’s count', ticks: 11, hz: 20, want: 11 },
    {
      name: 'before a sheet, the class’s 600 ms in whole ticks at 12 Hz',
      ticks: 0,
      hz: 12,
      want: 8,
    },
    { name: 'and at 20 Hz', ticks: 0, hz: 20, want: 12 },
    { name: 'a bowman’s 750 ms at 12 Hz', ticks: 0, hz: 12, want: 9, cls: 'bowman' },
  ])('the cooldown is $name', ({ ticks, hz, want, cls = 'warrior' }) => {
    const sheet = ticks > 0 ? create(StatSheetSchema, { attackCooldownTicks: ticks }) : undefined;
    expect(attackCooldownTicks(sheet, cls, 1000 / hz)).toBe(want);
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
