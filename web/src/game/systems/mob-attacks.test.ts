import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';
import type { Box, Geometry } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { CARRY_BLEND_MS, IMPACT_MS, MobAttacks } from './mob-attacks';
import type { Heard, MobAttack, Point, Upright } from './mob-attacks';
import type { MobState } from './mobs';

const TICK = 50;
const EAST = Math.PI / 2;

/** The first mob whose first attack is of kind. */
function mobWith(kind: MobAttack['kind']) {
  const m = mobs.find((x) => x.attacks[0]?.kind === kind);
  if (!m) {
    throw new Error(`no mob in the data attacks first with a ${kind}`);
  }
  return { type: m.id, height: m.height, radius: m.radius, attack: m.attacks[0] };
}

const leaper = mobWith('leap');
const charger = mobWith('rush');
const spitter = mobWith('shot');

/** Flat ground 40 m square, with boxes. */
const ground = (boxes: Box[] = []): Geometry => ({
  size_x: 40,
  size_z: 40,
  terrain: terrain(40, 2, 2, [0, 0, 0, 0], 1),
  boxes,
  cylinders: [],
});

const at = (type: string, x: number, pose = MobPose.UNSPECIFIED): MobState => ({
  type,
  x,
  y: 0,
  z: 20,
  yaw: EAST,
  hp: 10,
  maxHp: 10,
  pose,
  poseMs: 0,
});

const shownAs = (...shown: [number, MobState][]) => new Map(shown);
const landing = { x: 12, y: 0, z: 20 };

/** What the frames said of the mobs, ahead of the drawn ones, as a test sets it. */
class Frames implements Heard {
  readonly places = new Map<string, Point>();
  readonly poses = new Map<number, { t: number; pose: MobPose }[]>();
  readonly gone = new Map<number, number>();
  /** The newest place set at or before tick, as the tracker keeps them. */
  placeAt(mob: number, tick: number): (Point & { tick: number }) | undefined {
    for (let k = tick; k > tick - 400; k--) {
      const p = this.places.get(`${mob} ${k}`);
      if (p) {
        return { ...p, tick: k };
      }
    }
    return undefined;
  }
  posesOf(mob: number) {
    return this.poses.get(mob) ?? [];
  }
  goneAt(mob: number): number | undefined {
    return this.gone.get(mob);
  }
  pose(mob: number, t: number, pose: MobPose): void {
    this.poses.set(mob, [...this.posesOf(mob), { t, pose }]);
  }
}

/** A mob of type at x 10 winding up at tick 100 (5000 ms) toward the east, its first frame drawn at the render time 4800 and the player's clock at 5000. */
function windingUp(type: string, to: Point = landing, tickMs = TICK, boxes: Box[] = []) {
  const frames = new Frames();
  const a = new MobAttacks(tickMs, ground(boxes), frames);
  const shown = shownAs([7, at(type, 10)]);
  a.heardWindup(100, 7, 0, EAST, to);
  const from = 100 * tickMs;
  a.frame(from - 200, from, shown);
  return { a, frames, shown, from };
}

describe('MobAttacks', () => {
  it('shows a windup once the player’s clock reaches its tick, ahead of the drawn mobs, its area where the mob stood at that tick', () => {
    const frames = new Frames();
    frames.places.set('7 100', { x: 10.5, y: 0, z: 20 });
    const a = new MobAttacks(TICK, ground(), frames);
    a.heardWindup(100, 7, 0, EAST, landing);
    const shown = shownAs([7, at(leaper.type, 10)]);
    expect(a.frame(4800, 4999, shown).swings.size).toBe(0);
    const hit = 5000 + leaper.attack.windup_ms;
    expect(a.frame(4800, 5000, shown).swings.get(7)).toEqual({
      mob: 7,
      attack: leaper.attack,
      yaw: EAST,
      from: 5000,
      hit,
      end: hit + leaper.attack.travel_ms,
      at: { x: 10.5, y: 0, z: 20 },
      to: landing,
    });
    expect(a.frame(4900, 5100, shownAs([7, at(leaper.type, 11)])).swings.get(7)?.at.x).toBe(10.5); // fixed as it began
  });

  it('times its hit and its act’s end in whole ticks, rounded up, as the world counts them', () => {
    const { a, shown } = windingUp(leaper.type, landing, 70); // 600 ms of 70 ms ticks is 8.6, so 9
    const s = a.frame(6900, 7000, shown).swings.get(7);
    expect([s?.hit, s?.end]).toEqual([7000 + 9 * 70, 7000 + 9 * 70 + 6 * 70]); // and 400 ms is 5.7, so 6
  });

  it('a windup for a mob not drawn yet waits for it', () => {
    const a = new MobAttacks(TICK, ground(), new Frames());
    a.heardWindup(100, 7, 0, EAST, landing);
    expect(a.frame(4800, 5000, shownAs()).swings.size).toBe(0);
    expect(a.frame(4850, 5050, shownAs([7, at(leaper.type, 10)])).swings.get(7)?.from).toBe(5000);
  });

  it('a windup for a mob or attack this client does not know shows nothing', () => {
    const a = new MobAttacks(TICK, ground(), new Frames());
    a.heardWindup(100, 7, 5, EAST, landing);
    a.heardWindup(100, 8, 0, EAST, landing);
    const shown = shownAs([7, at(leaper.type, 10)], [8, at('from_a_newer_server', 14)]);
    expect(a.frame(5000, 5000, shown).swings.size).toBe(0);
  });

  describe('its result', () => {
    const bite = mobWith('front');

    it('waits past its hit on the player’s clock till the world says it acted: the drawn mobs reaching its hit', () => {
      const { a, shown, from } = windingUp(bite.type);
      const hit = from + bite.attack.windup_ms;
      expect(a.frame(hit - 200, hit + 50, shown).swings.get(7)?.landed).toBeUndefined();
      expect(a.frame(hit, hit + 200, shown).swings.get(7)?.landed).toBe(hit + 200);
    });

    it('or its Damage, heard before the drawn mobs get there', () => {
      const { a, shown, from } = windingUp(bite.type);
      const hit = from + bite.attack.windup_ms;
      a.struckBy(hit / TICK - 1, 7); // a spit from before: not this one's
      expect(a.frame(hit - 150, hit + 50, shown).swings.get(7)?.landed).toBeUndefined();
      a.struckBy(hit / TICK, 7);
      expect(a.frame(hit - 100, hit + 100, shown).swings.get(7)?.landed).toBe(hit + 100);
    });

    it('a hop’s waits for its landing, at its act’s end', () => {
      const { a, shown, from } = windingUp(leaper.type);
      const end = from + leaper.attack.windup_ms + leaper.attack.travel_ms;
      expect(a.frame(end - 50, end + 150, shown).swings.get(7)?.landed).toBeUndefined();
      expect(a.frame(end, end + 200, shown).swings.get(7)?.landed).toBe(end + 200);
    });

    it('is over once its impact has shown and its mob is back where it is drawn', () => {
      const { a, shown, from } = windingUp(bite.type);
      const hit = from + bite.attack.windup_ms;
      a.frame(hit, hit + 200, shown);
      expect(a.frame(hit + CARRY_BLEND_MS, hit + 200 + IMPACT_MS - 1, shown).swings.size).toBe(1);
      expect(a.frame(hit + CARRY_BLEND_MS, hit + 200 + IMPACT_MS, shown).swings.size).toBe(0);
    });
  });

  describe('a blow or death heard before its result', () => {
    it('a flinch or knockdown in its windup breaks it off, though the player’s clock is past its hit', () => {
      for (const pose of [MobPose.FLINCH, MobPose.DOWN]) {
        const { a, frames, shown, from } = windingUp(spitter.type);
        const hit = from + spitter.attack.windup_ms;
        expect(a.frame(hit - 150, hit + 100, shown).spits).toHaveLength(1); // thrown on the player's clock
        frames.pose(7, hit - 50, pose);
        const drawn = a.frame(hit - 100, hit + 150, shown);
        expect([drawn.swings.size, drawn.spits.length]).toEqual([0, 0]); // its ball taken back
      }
    });

    it('a flinch from before its windup breaks nothing', () => {
      const { a, frames, shown, from } = windingUp(leaper.type);
      frames.pose(7, from - 50, MobPose.FLINCH);
      expect(a.frame(from, from + 200, shown).swings.size).toBe(1);
    });

    it('a flinch in a hop’s act leaves it; a knockdown there breaks it', () => {
      const { a, frames, shown, from } = windingUp(leaper.type);
      const hit = from + leaper.attack.windup_ms;
      frames.pose(7, hit + 50, MobPose.FLINCH);
      expect(a.frame(hit, hit + 200, shown).swings.size).toBe(1);
      frames.pose(7, hit + 100, MobPose.DOWN);
      expect(a.frame(hit + 50, hit + 250, shown).swings.size).toBe(0);
    });

    it('its mob’s death by its act’s end breaks it; after its result, it shows on', () => {
      const { a, frames, shown, from } = windingUp(leaper.type);
      const end = from + leaper.attack.windup_ms + leaper.attack.travel_ms;
      frames.gone.set(7, end);
      expect(a.frame(end - 100, end + 100, shown).swings.size).toBe(0); // dropped with it, as the world drops its landing
      const after = windingUp(leaper.type);
      after.a.frame(end, end + 200, after.shown);
      after.frames.gone.set(7, end + 50);
      expect(after.a.frame(end + 50, end + 250, after.shown).swings.get(7)?.landed).toBe(end + 200);
    });

    it("a mob's next windup takes its place, and its leaving the drawn mobs ends it", () => {
      const { a, shown } = windingUp(leaper.type);
      a.heardWindup(110, 7, 0, -EAST, landing);
      expect(a.frame(5300, 5500, shown).swings.get(7)).toMatchObject({ yaw: -EAST, from: 5500 });
      expect(a.frame(5350, 5550, shownAs()).swings.size).toBe(0);
    });
  });

  describe('a hop or a charge carries its mob on the player’s clock', () => {
    const hit = 5000 + leaper.attack.windup_ms;
    const end = hit + leaper.attack.travel_ms;

    it('blended from where it is drawn as its windup starts, held where it stood till its hit, to its landing by its end', () => {
      const { a, shown } = windingUp(leaper.type);
      const drawnAt = shownAs([7, at(leaper.type, 9)]); // the drawn mob, behind, still walking up
      expect(a.frame(4800 + CARRY_BLEND_MS / 2, 5000 + 125, drawnAt).carried.get(7)?.x).toBeCloseTo(
        9.5,
      ); // halfway from 9 to 10 on the render clock, though the player's clock jumped a tick
      expect(a.frame(4800 + CARRY_BLEND_MS, hit, drawnAt).carried.get(7)).toMatchObject({
        x: 10,
        z: 20,
        yaw: EAST,
      });
      expect(a.frame(4950, hit + leaper.attack.travel_ms / 2, shown).carried.get(7)?.x).toBeCloseTo(
        11,
      );
      expect(a.frame(5000, end + 100, shown).carried.get(7)?.x).toBeCloseTo(12);
    });

    it('back to where it is drawn once the drawn mobs reach its end', () => {
      const { a, shown } = windingUp(leaper.type);
      const landed = shownAs([7, at(leaper.type, 11.8)]); // the world landed it a little short
      a.frame(end - 100, end + 100, landed);
      expect(a.frame(end + CARRY_BLEND_MS / 2, end + 300, landed).carried.get(7)?.x).toBeCloseTo(
        11.9,
      );
      expect(a.frame(end + CARRY_BLEND_MS, end + 400, landed).carried.has(7)).toBe(false);
      expect(a.frame(end + CARRY_BLEND_MS, end + 300 + IMPACT_MS, shown).swings.size).toBe(0); // its landing shown from end + 300
    });

    it('back from where it was carried when a knockdown breaks it', () => {
      const { a, frames, shown } = windingUp(leaper.type);
      a.frame(hit - 100, hit + leaper.attack.travel_ms / 2, shown); // carried to 11
      frames.pose(7, hit + 50, MobPose.DOWN);
      expect(a.frame(hit - 50, hit + 250, shown).carried.get(7)?.x).toBeCloseTo(11);
      expect(
        a.frame(hit - 50 + CARRY_BLEND_MS / 2, hit + 300, shown).carried.get(7)?.x,
      ).toBeCloseTo(10.5);
      expect(a.frame(hit - 50 + CARRY_BLEND_MS, hit + 350, shown).carried.has(7)).toBe(false);
    });

    it('a charge stops where it meets the player, as the world’s does', () => {
      const chargeTo = { x: 14, y: 0, z: 20 };
      const { a, shown } = windingUp(charger.type, chargeTo);
      const me: Upright = { x: 12.5, y: 0, z: 20, radius: 0.375, height: 1.75 };
      const chit = 5000 + charger.attack.windup_ms;
      const touching = 12.5 - 0.375 - charger.radius;
      const c = a.frame(chit - 100, chit + charger.attack.travel_ms, shown, me).carried.get(7);
      expect(c?.x).toBeCloseTo(touching);
      expect(
        a.frame(chit, chit + charger.attack.travel_ms + 50, shown, { ...me, x: 20 }).carried.get(7)
          ?.x,
      ).toBeCloseTo(touching); // and stays
    });

    it('a charge looks for the player only once it runs: one beside its start in its windup, gone by its hit, never holds it there', () => {
      const chargeTo = { x: 14, y: 0, z: 20 };
      const chit = 5000 + charger.attack.windup_ms;
      const beside: Upright = {
        x: 10 + charger.radius + 0.3,
        y: 0,
        z: 20,
        radius: 0.375,
        height: 1.75,
      }; // touching its start
      const jumping = { ...beside, y: 1.8 }; // over its start, as a player's jump peaks
      const gone = { ...beside, z: 24 };
      for (const me of [beside, jumping]) {
        const { a, shown } = windingUp(charger.type, chargeTo);
        a.frame(4900, (5000 + chit) / 2, shown, me);
        expect(
          a.frame(4950, chit + charger.attack.travel_ms, shown, gone).carried.get(7)?.x,
        ).toBeCloseTo(14);
      }
    });

    it('a charge meets only a player level with its body, and runs under one clear above it', () => {
      const chargeTo = { x: 14, y: 0, z: 20 };
      const chit = 5000 + charger.attack.windup_ms;
      const run = (y: number) => {
        const { a, shown } = windingUp(charger.type, chargeTo);
        const me: Upright = { x: 12.5, y, z: 20, radius: 0.375, height: 1.75 };
        return a.frame(chit - 100, chit + charger.attack.travel_ms, shown, me).carried.get(7)?.x;
      };
      expect(run(charger.height + 0.05)).toBeCloseTo(14);
      expect(run(charger.height - 0.05)).toBeCloseTo(12.5 - 0.375 - charger.radius);
    });

    it('a charge that ran past the player is not pulled back when they step into its lane behind it, nor after its run', () => {
      const chargeTo = { x: 14, y: 0, z: 20 };
      const chit = 5000 + charger.attack.windup_ms;
      const half = chit + charger.attack.travel_ms * 0.75; // run to 13
      const aside: Upright = { x: 12, y: 0, z: 22, radius: 0.375, height: 1.75 }; // dodged the lane
      const behind = { ...aside, z: 20, x: 11.5 }; // back in the lane, short of the mob at 13
      const mid = windingUp(charger.type, chargeTo);
      mid.a.frame(chit - 300, half, mid.shown, aside);
      expect(
        mid.a.frame(chit - 280, chit + charger.attack.travel_ms, mid.shown, behind).carried.get(7)
          ?.x,
      ).toBeCloseTo(14);
      const after = windingUp(charger.type, chargeTo);
      after.a.frame(chit - 300, chit + charger.attack.travel_ms, after.shown, aside); // run over at 14
      expect(
        after.a
          .frame(chit - 280, chit + charger.attack.travel_ms + 100, after.shown, behind)
          .carried.get(7)?.x,
      ).toBeCloseTo(14);
    });
  });

  describe('a charge meets the mobs drawn in its lane, as the world’s does', () => {
    const chargeTo = { x: 14, y: 0, z: 20 };
    const chit = 5000 + charger.attack.windup_ms;
    const ends = chit + charger.attack.travel_ms;
    const r = 2 * charger.radius; // two chargers' centres apart as they touch
    const run = (other: MobState, me?: Upright) => {
      const { a } = windingUp(charger.type, chargeTo);
      const shown = shownAs([7, at(charger.type, 10)], [8, other]); // 8 stepped in after the windup
      return a.frame(chit, ends, shown, me).carried.get(7)?.x;
    };
    const mushroom = (x: number, z = 20): MobState => ({ ...at(charger.type, x), z });

    it('stops touching one that stepped into its lane during its windup', () => {
      expect(run(mushroom(12.5))).toBeCloseTo(12.5 - r);
    });

    it('passes one it only grazes, and stops at one a little further in', () => {
      expect(run(mushroom(12.5, 20 + r - 0.2))).toBeCloseTo(14);
      expect(run(mushroom(12.5, 20 + r - 0.3))).toBeLessThan(12.5);
    });

    it('passes one behind it, or one clear above it', () => {
      expect(run(mushroom(9))).toBeCloseTo(14);
      expect(run({ ...mushroom(12.5), y: charger.height })).toBeCloseTo(14);
    });

    describe("sweeps the mobs where the world has them on the player's clock, not as drawn a moment behind", () => {
      const tick = chit / TICK;
      const sweep = (
        drawnZ: number,
        frames: [number, number][],
        at = ends,
        others: [number, MobState][] = [],
      ) => {
        const w = windingUp(charger.type, chargeTo);
        for (const [k, z] of frames) {
          w.frames.places.set(`8 ${k}`, { x: 12.5, y: 0, z });
        }
        const shown = shownAs([7, at === ends ? at7 : at7], [8, mushroom(12.5, drawnZ)], ...others);
        return w.a.frame(chit, at, shown).carried.get(7)?.x;
      };
      const at7 = at(charger.type, 10);

      it('runs on past one drawn in its lane that the frames show gone out of it', () => {
        expect(sweep(20, [[tick, 21.6]])).toBeCloseTo(14);
      });

      it('stops at one drawn beside its lane that the frames show stepped in', () => {
        expect(sweep(21.6, [[tick, 20]])).toBeCloseTo(12.5 - r);
      });

      it('moves one on from its last frame by its velocity there, for at most 200 ms', () => {
        // last frames 2 ticks before the player's clock, leaving at 5 m/s: 0.5 m on by the hit, out of the lane
        expect(
          sweep(20, [
            [tick - 3, 20.15],
            [tick - 2, 20.4],
          ]),
        ).toBeCloseTo(14);
        // its last frames 1 s old, leaving at 0.5 m/s: carried on 200 ms, 0.1 m, so still in the lane, cut as it touches
        const off = 0.125;
        expect(
          sweep(20, [
            [tick - 21, 20],
            [tick - 20, 20.025],
          ]),
        ).toBeCloseTo(12.5 - Math.sqrt(r * r - off * off), 3);
      });

      it('sweeps a mob that is charging itself where its own run is drawn', () => {
        const w = windingUp(charger.type, chargeTo);
        const windup = charger.attack.windup_ms / TICK;
        expect(windup).toBeGreaterThan(4); // so 8's windup is heard by the first frame
        w.a.heardWindup(100 - 4, 8, 0, 0, { x: 12.5, y: 0, z: 26 }); // its own charge north, 200 ms ahead of 7's
        const shown = shownAs([7, at7], [8, mushroom(12.5, 20)]); // drawn behind, still in the lane
        w.a.frame(chit - 600, chit - 400, shown);
        w.a.frame(chit - 300, chit - 100, shown); // 8 runs north, 1.5 m by now
        expect(w.a.frame(chit - 200, ends, shown).carried.get(7)?.x).toBeCloseTo(14);
      });
    });

    it('runs on behind another charge running ahead of it in its lane, swept where it is carried this frame', () => {
      // finga-player's k29: two mushrooms charging east in a line 1.26 m apart, 0.135 m clear of touching, at 60 fps
      const gap = 1.26;
      const speed = charger.attack.distance / charger.attack.travel_ms; // metres a millisecond
      expect(gap - r).toBeLessThan(speed * (1000 / 60)); // a frame stale would cut it
      const behind = (lead: number) => {
        const { a } = windingUp(charger.type, chargeTo);
        const from = 10 + gap - speed * lead * TICK; // a lead of ticks, run by 7's hit
        a.heardWindup(100 - lead, 8, 0, EAST, { x: from + charger.attack.distance, y: 0, z: 20 });
        const shown = shownAs([7, at(charger.type, 10)], [8, mushroom(from)]);
        const done = ends - lead * TICK; // as the one ahead ends its run
        for (let mine = 5000; mine < done; mine += 1000 / 60) {
          a.frame(mine - 200, mine, shown);
        }
        return a.frame(done - 200, done, shown).carried.get(7)?.x;
      };
      expect(behind(0)).toBeCloseTo(14); // both starting at once
      expect(behind(1)).toBeCloseTo(14 - speed * TICK); // the one ahead already running
    });

    it('meets the player first if they come before it would sink 0.25 m deeper into the mob, else stops at the mob', () => {
      const off = r - 0.3; // the mob brushes the lane, deep enough to cut it
      const cut = 3 - Math.sqrt(r * r - off * off); // along the run from 10, to the mob at 13
      const deep = 3 - Math.sqrt((r - 0.25) ** 2 - off * off);
      const player = (meet: number): Upright => ({
        x: 10 + meet + charger.radius + 0.375,
        y: 0,
        z: 20,
        radius: 0.375,
        height: 1.75,
      });
      expect(run(mushroom(13, 20 + off), player(deep - 0.05))).toBeCloseTo(10 + deep - 0.05);
      expect(run(mushroom(13, 20 + off), player(deep + 0.1))).toBeCloseTo(10 + cut);
    });
  });

  it('clear forgets every windup and spit', () => {
    const { a, shown } = windingUp(spitter.type);
    a.frame(4800, 5000 + spitter.attack.windup_ms, shown);
    a.heardWindup(200, 7, 0, EAST, landing);
    a.clear();
    expect(a.frame(10000, 10000, shown)).toEqual({
      swings: new Map(),
      spits: [],
      carried: new Map(),
    });
  });

  describe('a spit', () => {
    const { attack, height } = spitter;
    const thrown = 5000 + attack.windup_ms;
    const lag = 200; // the render clock behind the player's

    it("flies on the player's clock from where its mob stood at mid-height along its yaw, at its speed, once the windup ends", () => {
      const { a, shown } = windingUp(spitter.type);
      expect(a.frame(thrown - 1 - lag, thrown - 1, shown).spits).toEqual([]);
      const [s] = a.frame(thrown, thrown + 200, shownAs([7, at(spitter.type, 9)])).spits;
      expect(s).toMatchObject({ mob: 7, y: height / 2, z: 20, size: attack.size });
      expect(s?.x).toBeCloseTo(10 + 0.2 * attack.speed);
    });

    it('ends once it has flown its range', () => {
      const { a, shown } = windingUp(spitter.type);
      const ends = thrown + (attack.range / attack.speed) * 1000;
      expect(a.frame(ends - 1 - lag, ends - 1, shown).spits).toHaveLength(1);
      expect(a.frame(ends - lag, ends, shown).spits).toEqual([]);
    });

    it('its windup says how far its ball will fly: short of a solid in its way, else its range', () => {
      const wall = { x: 13, z: 20, half_w: 0.25, half_d: 2, ux: 1, uz: 0, top: 3 };
      expect(
        windingUp(spitter.type, landing, TICK, [wall])
          .a.frame(4800, 5000, shownAs([7, at(spitter.type, 10)]))
          .swings.get(7)?.reach,
      ).toBeCloseTo(2.75 - attack.size / 2, 1);
      expect(
        windingUp(spitter.type)
          .a.frame(4800, 5000, shownAs([7, at(spitter.type, 10)]))
          .swings.get(7)?.reach,
      ).toBe(attack.range);
      expect(
        windingUp(leaper.type)
          .a.frame(4800, 5000, shownAs([7, at(leaper.type, 10)]))
          .swings.get(7)?.reach,
      ).toBeUndefined();
    });

    it('ends at a solid in its way, and flies over a low one', () => {
      const wall = { x: 13, z: 20, half_w: 0.25, half_d: 2, ux: 1, uz: 0, top: 3 };
      const { a, shown } = windingUp(spitter.type, landing, TICK, [wall]);
      const after = (d: number) => thrown + (d / attack.speed) * 1000;
      expect(a.frame(after(2.4) - lag, after(2.4), shown).spits).toHaveLength(1);
      expect(a.frame(after(2.6) - lag, after(2.6), shown).spits).toEqual([]); // its ball meets the wall's west face 2.75 m out, less its radius
      const low = windingUp(spitter.type, landing, TICK, [{ ...wall, top: 0.1 }]);
      expect(low.a.frame(after(4) - lag, after(4), low.shown).spits).toHaveLength(1);
    });

    it('ends on the player as it meets them, as the world’s lands', () => {
      const { a, shown } = windingUp(spitter.type);
      const me: Upright = { x: 13, y: 0, z: 20, radius: 0.375, height: 1.75 };
      const meets = thrown + ((3 - 0.375 - attack.size / 2) / attack.speed) * 1000;
      expect(a.frame(meets - 30 - lag, meets - 30, shown, me).spits).toHaveLength(1);
      expect(a.frame(meets + 10 - lag, meets + 10, shown, me).spits).toEqual([]);
      expect(a.frame(meets + 20 - lag, meets + 20, shown, { ...me, x: 30 }).spits).toEqual([]); // gone for good
    });

    it("ends at the mob's Damage, heard while it flies or before it was thrown", () => {
      const { a, shown } = windingUp(spitter.type);
      a.frame(thrown - lag, thrown + 50, shown);
      a.struckBy(thrown / TICK + 4, 7);
      expect(a.frame(thrown - lag, thrown + 199, shown).spits).toHaveLength(1);
      expect(a.frame(thrown - lag, thrown + 200, shown).spits).toEqual([]);
      const b = windingUp(spitter.type);
      b.a.struckBy(thrown / TICK + 2, 7);
      expect(b.a.frame(thrown - lag, thrown + 99, b.shown).spits).toHaveLength(1);
      expect(b.a.frame(thrown - lag, thrown + 100, b.shown).spits).toEqual([]);
    });

    it('a Damage from another mob, or from before its throw, leaves it flying', () => {
      const { a, shown } = windingUp(spitter.type);
      a.struckBy(5000 / TICK + 1, 7);
      a.struckBy(thrown / TICK + 2, 8);
      expect(a.frame(thrown - lag, thrown + 150, shown).spits).toHaveLength(1);
    });

    it('ends where its mob died, heard after', () => {
      const { a, frames, shown } = windingUp(spitter.type);
      a.frame(thrown - lag, thrown + 150, shown);
      frames.gone.set(7, thrown + 100);
      expect(a.frame(thrown - 100, thrown + 200, shown).spits).toEqual([]);
    });
  });
});
