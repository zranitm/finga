import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { MobPose, MobViewSchema } from '@/protocol/messages';

import {
  DamageNumbers,
  InvulnFlash,
  MobTracker,
  NUMBER_LIFE_MS,
  numberFor,
  parseRef,
} from './combat-view';
import { HISTORY_MS } from './prediction';

const slime = (id: number, x: number, hp = 30, pose = MobPose.UNSPECIFIED) =>
  create(MobViewSchema, { id, type: 'green_slime', x, y: 608, facing: -1, hp, maxHp: 30, pose });

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
  it("shows a mob's pose from the render time of the frame that set it, with how long it has held", () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 100)]);
    m.applyDelta(1050, [slime(1, 100, 25, MobPose.FLINCH)], []);
    m.applyDelta(1350, [slime(1, 108, 25)], []);
    expect(m.render(1025).shown.get(1)).toMatchObject({ pose: MobPose.UNSPECIFIED, poseMs: 25 });
    expect(m.render(1100).shown.get(1)).toMatchObject({ pose: MobPose.FLINCH, poseMs: 50 });
    expect(m.render(1400).shown.get(1)).toMatchObject({ pose: MobPose.UNSPECIFIED, poseMs: 50 });
  });

  it('a player who arrives sees a mob that is down', () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 100, 25, MobPose.DOWN)]);
    expect(m.render(1000).shown.get(1)).toMatchObject({ pose: MobPose.DOWN });
  });

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
      pose: MobPose.UNSPECIFIED,
      poseMs: 50,
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

  // A green slime is 32 by 28, its feet at (x, 608).
  const box = (x: number) => ({ x: x - 16, y: 608 - 28, w: 32, h: 28 });

  it('boxesAt gives the mobs as the frame of a tick left them: one not yet there, or gone from that tick, blocks nothing', () => {
    const m = new MobTracker(50);
    m.applySnapshot(10 * 50, [slime(1, 100)]);
    m.applyDelta(11 * 50, [slime(1, 102), slime(2, 300)], []);
    m.applyDelta(12 * 50, [], [1]);
    expect(m.boxesAt(9)).toEqual([]);
    expect(m.boxesAt(10)).toEqual([box(100)]);
    expect(m.boxesAt(11)).toEqual([box(102), box(300)]);
    expect(m.boxesAt(12)).toEqual([box(300)]);
    expect(m.boxesAt(13)).toEqual([box(300)]); // unchanged since 11
  });

  it('keeps every box a replayed input may name, after its mob has drained from the screen, then lets them go', () => {
    const m = new MobTracker(50);
    m.applySnapshot(0, [slime(1, 100), slime(2, 200)]);
    m.applyDelta(50, [], [1]);
    m.render(1000); // slime 1 drained from the screen
    expect(m.boxesAt(0)).toEqual([box(100), box(200)]);
    const keep = Math.ceil((HISTORY_MS + 500) / 50);
    m.applyDelta((keep + 1) * 50, [slime(3, 300)], []);
    expect(m.boxesAt(0)).toEqual([box(200)]); // slime 1 let go; slime 2, still since, keeps its last position
    expect(m.boxesAt(keep + 1)).toEqual([box(200), box(300)]);
    m.applySnapshot(5000, []); // a new map or a rejoin
    expect(m.boxesAt(keep + 1)).toEqual([]);
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
    expect(b).toMatchObject({ x: 104, y: 600 - 5 - 16 }); // a has risen 5 px by then
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

  it('a number on a foe up on a platform and one on the ground beneath stand apart: a spot is 2 lines up or down, from where each was asked to start', () => {
    const n = new DamageNumbers();
    n.spawn(500, 600, '71', 'hit', 1000);
    expect(n.spawn(505, 600 - 120, '9', 'hit', 1000).y).toBe(480); // the platform's
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
