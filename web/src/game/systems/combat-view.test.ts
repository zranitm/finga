import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { MobViewSchema } from '@/protocol/messages';

import {
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  NUMBER_LIFE_MS,
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
