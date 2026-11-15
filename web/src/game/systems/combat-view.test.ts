import { describe, expect, it } from 'vitest';

import { DamageNumbers, InvulnFlash, NUMBER_LIFE_MS, numberFor, parseRef } from './combat-view';

describe('parseRef', () => {
  it.each([
    { ref: 'p:c-ayla', want: { kind: 'player', id: 'c-ayla' } },
    { ref: 'm:7', want: { kind: 'mob', id: 7 } },
    { ref: 'm:x', want: undefined },
    { ref: 'm:-1', want: undefined },
    { ref: 'p:', want: undefined },
    { ref: 'q:1', want: undefined },
  ])('$ref', ({ ref, want }) => {
    expect(parseRef(ref)).toEqual(want);
  });
});

describe('numberFor and DamageNumbers', () => {
  it.each([
    { name: 'a hit', d: { amount: 7 }, me: false, want: { text: '7', kind: 'hit' } },
    {
      name: 'a crit',
      d: { amount: 14, crit: true },
      me: false,
      want: { text: '14!', kind: 'crit' },
    },
    {
      name: 'a miss',
      d: { amount: 0, miss: true },
      me: false,
      want: { text: 'MISS', kind: 'miss' },
    },
    { name: 'a hit on me', d: { amount: 5 }, me: true, want: { text: '-5', kind: 'taken' } },
  ])('$name', ({ d, me, want }) => {
    expect(numberFor(d, me)).toEqual(want);
  });

  it('queues one number per spawn, rises and fades, and expires', () => {
    const n = new DamageNumbers();
    n.spawn(100, 600, '7', 'hit', 1000);
    const start = n.update(1000)[0]!;
    expect([start.dy, start.alpha, start.lift]).toEqual([0, 1, 0]);
    const mid = n.update(1000 + NUMBER_LIFE_MS * 0.75)[0]!;
    expect(mid.dy).toBeCloseTo(-30);
    expect(mid.alpha).toBeCloseTo(0.5);
    expect(n.update(1000 + NUMBER_LIFE_MS)).toEqual([]);
    n.spawn(1, 1, 'MISS', 'miss', 2000);
    n.clear();
    expect(n.update(2000)).toEqual([]);
  });

  it.each([0, 16, 33])(
    'shows 1.4 times its size on the frame that first draws it, %d ms after the hit, and eases evenly to its size 120 ms on',
    (late) => {
      const n = new DamageNumbers();
      n.spawn(100, 600, '7', 'hit', 1000);
      const first = 1000 + late;
      const scale = (t: number) => n.update(t)[0]!.scale;
      expect(scale(first)).toBeCloseTo(1.4);
      expect(scale(first + 30)).toBeCloseTo(1 + 0.4 * 0.854, 3); // still most of the pop a quarter of the way in
      expect(scale(first + 60)).toBeCloseTo(1.2);
      expect(scale(first + 90)).toBeCloseTo(1 + 0.4 * 0.146, 3);
      expect(scale(first + 120)).toBe(1);
      expect(scale(first + 500)).toBe(1);
    },
  );

  it("a number within 300 ms of another on its target starts a line above it as it's drawn, so a chain stacks upward", () => {
    const n = new DamageNumbers();
    const a = n.spawn(100, 600, '7', 'hit', 1000);
    const b = n.spawn(104, 600, '8', 'hit', 1100);
    expect(b).toMatchObject({ x: 104, y: 600 - 5 - 16, lift: 5 + 16 }); // a has risen 5 px by then
    expect(n.spawn(100, 600, '3', 'crit', 1399).y).toBeCloseTo(b.y - (40 * 299) / 800 - 16);
    n.update(1700);
    expect(n.spawn(100, 600, '4', 'hit', 1700).y).toBe(600); // 300 ms on: it starts afresh
    expect(a.y).toBe(600);
  });

  it('numbers on piled foes stack by place: within 24 px across of one under 300 ms old, a line above the highest; further off, where they fall', () => {
    const n = new DamageNumbers();
    const slime = n.spawn(500, 600, '71', 'hit', 1000);
    const piled = n.spawn(510, 600, '69', 'hit', 1000); // Arc on a second slime in the pile
    expect(piled.y).toBe(600 - 16);
    expect(n.spawn(490, 600, '70', 'hit', 1000).y).toBe(600 - 32); // above the highest
    expect(n.spawn(525, 600, '68', 'hit', 1000).y).toBe(600 - 32); // 15 px from the second, 25 from the first
    expect(n.spawn(560, 600, '12', 'hit', 1000).y).toBe(600); // 35 px from the nearest
    expect(slime.y).toBe(600);
  });

  it('a number on a foe further up the screen and one beneath it stand apart: a spot is 2 lines up or down, from where each was asked to start', () => {
    const n = new DamageNumbers();
    n.spawn(500, 600, '71', 'hit', 1000);
    expect(n.spawn(505, 600 - 120, '9', 'hit', 1000).y).toBe(480);
    const below = (dy: number) => {
      const q = new DamageNumbers();
      q.spawn(500, 600, '71', 'hit', 1000);
      return q.spawn(495, 600 + dy, '8', 'hit', 1000).y;
    };
    expect(below(32)).toBe(600 - 16); // 2 lines down: its spot, so a line above the 71
    expect(below(33)).toBe(633); // past 2 lines: where it fell
    const chain = [1050, 1100, 1150, 1200].map((t) => n.spawn(700, 600, '1', 'hit', t).y);
    expect(chain.every((y, k) => k === 0 || y < chain[k - 1]! - 15)).toBe(true); // a chain keeps stacking, each a line up
  });
});

describe('InvulnFlash', () => {
  it('a state with invulnerable: true starts the blink, and one without it ends it', () => {
    const f = new InvulnFlash();
    expect(f.shown(0)).toBe(true);
    f.set(true, 1000);
    expect(f.shown(1000)).toBe(false);
    expect(f.shown(1150)).toBe(true);
    expect(f.shown(1250)).toBe(false);
    f.set(true, 1300); // still on: the phase is kept
    expect(f.shown(1350)).toBe(true);
    f.set(false, 1400);
    expect(f.shown(1400)).toBe(true);
  });
});
