import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { MobPose, MobViewSchema } from '@/protocol/messages';

import { MobTracker } from './mobs';
import { HISTORY_MS } from './prediction';

const slime = (id: number, x: number, hp = 30, pose = MobPose.UNSPECIFIED, yaw = 0) =>
  create(MobViewSchema, { id, type: 'green_slime', x, y: 1, z: 20, yaw, hp, maxHp: 30, pose });

// A green slime's circle: 0.5 m round and 0.875 m tall, its feet at (x, 1, 20).
const circle = (x: number) => ({ x, z: 20, radius: 0.5, bottom: 1, top: 1.875 });

describe('MobTracker', () => {
  it("shows a mob's pose from the render time of the frame that set it, with how long it has held", () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 10)]);
    m.applyDelta(1050, [slime(1, 10, 25, MobPose.FLINCH)], []);
    m.applyDelta(1350, [slime(1, 10.25, 25)], []);
    expect(m.render(1025).shown.get(1)).toMatchObject({ pose: MobPose.UNSPECIFIED, poseMs: 25 });
    expect(m.render(1100).shown.get(1)).toMatchObject({ pose: MobPose.FLINCH, poseMs: 50 });
    expect(m.render(1400).shown.get(1)).toMatchObject({ pose: MobPose.UNSPECIFIED, poseMs: 50 });
  });

  it('a player who arrives sees a mob that is down', () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 10, 25, MobPose.DOWN)]);
    expect(m.render(1000).shown.get(1)).toMatchObject({ pose: MobPose.DOWN });
  });

  it('a Snapshot sets the mobs and drops the rest; Deltas update and remove', () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 10), slime(2, 20)]);
    m.applyDelta(1050, [slime(1, 11)], [2]);
    expect(m.render(1025).shown.get(1)?.x).toBe(10.5);
    expect(m.render(1049).shown.has(2)).toBe(true);
    const { shown, gone } = m.render(1050);
    expect(gone).toEqual([2]);
    expect(shown.get(1)).toEqual({
      type: 'green_slime',
      x: 11,
      y: 1,
      z: 20,
      yaw: 0,
      hp: 30,
      maxHp: 30,
      pose: MobPose.UNSPECIFIED,
      poseMs: 50,
    });
    m.applySnapshot(1100, [slime(3, 30)]);
    expect([...m.render(1100).shown.keys()]).toEqual([3]);
  });

  it('a Snapshot starts over: a new instance’s ids and ticks begin afresh, and its frames draw', () => {
    const m = new MobTracker(50);
    m.applySnapshot(5000, [slime(1, 10)]);
    m.applyDelta(5050, [slime(1, 11)], []);
    m.applySnapshot(1000, [slime(1, 40)]); // the map restarted: mob 1 is another mob, at earlier ticks
    m.applyDelta(1050, [slime(1, 41)], []);
    expect(m.render(1025).shown.get(1)?.x).toBe(40.5);
    expect(m.blockersAt(20)).toEqual([circle(40)]);
  });

  it('blends a mob between frames in x, y and z, turns it the short way, and holds it past the last', () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [
      create(MobViewSchema, { id: 1, type: 'green_slime', x: 10, y: 0, z: 20, yaw: 0 }),
    ]);
    m.applyDelta(
      1050,
      [create(MobViewSchema, { id: 1, type: 'green_slime', x: 12, y: 1, z: 18, yaw: 3 })],
      [],
    );
    expect(m.render(1010).shown.get(1)).toMatchObject({ x: 10.4, y: 0.2, z: 19.6 });
    expect(m.render(1010).shown.get(1)?.yaw).toBeCloseTo(0.6);
    expect(m.render(1040).shown.get(1)).toMatchObject({ x: 11.6, y: 0.8, z: 18.4 });
    expect(m.render(1040).shown.get(1)?.yaw).toBeCloseTo(2.4);
    expect(m.render(1090).shown.get(1)).toMatchObject({ x: 12, y: 1, z: 18, yaw: 3 }); // no velocity on the wire: it waits for the next frame
    m.applyDelta(
      1100,
      [create(MobViewSchema, { id: 1, type: 'green_slime', x: 12, y: 1, z: 18, yaw: -3 })],
      [],
    );
    expect(m.render(1075).shown.get(1)?.yaw).toBeCloseTo(Math.PI); // from 3 to -3 through π, not back through 0
  });

  it("shows a mob's HP as the server last sent it, and a killed mob at 0 until it drains", () => {
    const m = new MobTracker(50);
    m.applySnapshot(1000, [slime(1, 10), slime(2, 20)]);
    m.applyDelta(1050, [slime(1, 10, 18)], [2, 99]); // 99: unknown, ignored
    const { shown } = m.render(1025);
    expect(shown.get(1)?.hp).toBe(18);
    expect(shown.get(2)?.hp).toBe(0);
    m.clear();
    expect(m.render(1025).shown.size).toBe(0);
  });

  it('blockersAt gives the mobs as the frame of a tick left them: one not yet there, or gone from that tick, blocks nothing', () => {
    const m = new MobTracker(50);
    m.applySnapshot(10 * 50, [slime(1, 10)]);
    m.applyDelta(11 * 50, [slime(1, 10.25), slime(2, 30)], []);
    m.applyDelta(12 * 50, [], [1]);
    expect(m.blockersAt(9)).toEqual([]);
    expect(m.blockersAt(10)).toEqual([circle(10)]);
    expect(m.blockersAt(11)).toEqual([circle(10.25), circle(30)]);
    expect(m.blockersAt(12)).toEqual([circle(30)]);
    expect(m.blockersAt(13)).toEqual([circle(30)]); // unchanged since 11
  });

  it('blockersAt lists the mobs in id order, as the world steps players against them', () => {
    const m = new MobTracker(50);
    m.applySnapshot(0, [slime(5, 10)]);
    m.applyDelta(50, [slime(2, 20)], []);
    expect(m.blockersAt(1)).toEqual([circle(20), circle(10)]);
  });

  it('a mob’s circle stands on its feet: up a slope or a solid, its bottom and top go with it', () => {
    const m = new MobTracker(50);
    m.applySnapshot(0, [create(MobViewSchema, { id: 1, type: 'king_slime', x: 5, y: 2.5, z: 6 })]);
    expect(m.blockersAt(0)).toEqual([{ x: 5, z: 6, radius: 1.5, bottom: 2.5, top: 2.5 + 2.625 }]);
  });

  it('keeps every circle a replayed input may name, after its mob has drained from the screen, then lets them go', () => {
    const m = new MobTracker(50);
    m.applySnapshot(0, [slime(1, 10), slime(2, 20)]);
    m.applyDelta(50, [], [1]);
    m.render(1000); // slime 1 drained from the screen
    expect(m.blockersAt(0)).toEqual([circle(10), circle(20)]);
    const keep = Math.ceil((HISTORY_MS + 500) / 50);
    m.applyDelta((keep + 1) * 50, [slime(3, 30)], []);
    expect(m.blockersAt(0)).toEqual([circle(20)]); // slime 1 let go; slime 2, still since, keeps its last place
    expect(m.blockersAt(keep + 1)).toEqual([circle(20), circle(30)]);
    m.applySnapshot(5000, []); // a new map or a rejoin
    expect(m.blockersAt(keep + 1)).toEqual([]);
  });
});
