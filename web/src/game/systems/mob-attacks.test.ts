import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';
import type { Box, Geometry } from '@/sim/step';
import { terrain } from '@/sim/terrain';

import { IMPACT_MS, MobAttacks } from './mob-attacks';
import type { MobAttack } from './mob-attacks';
import type { MobState } from './mobs';

const TICK = 50;
const EAST = Math.PI / 2;

/** The first mob whose first attack is of kind. */
function mobWith(kind: MobAttack['kind']) {
  const m = mobs.find((x) => x.attacks[0]?.kind === kind);
  if (!m) {
    throw new Error(`no mob in the data attacks first with a ${kind}`);
  }
  return { type: m.id, height: m.height, attack: m.attacks[0] };
}

const leaper = mobWith('leap');
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

describe('MobAttacks', () => {
  it('shows a windup once the render clock reaches its tick, its area fixed where the mob then stood, till its impact is over', () => {
    const a = new MobAttacks(TICK, ground());
    a.heard(100, 7, 0, EAST, landing);
    const shown = shownAs([7, at(leaper.type, 10)]);
    expect(a.frame(4999, shown).swings.size).toBe(0);
    expect(a.frame(5000, shown).swings.get(7)).toEqual({
      mob: 7,
      attack: leaper.attack,
      yaw: EAST,
      from: 5000,
      at: { x: 10, y: 0, z: 20 },
      to: landing,
    });
    expect(a.frame(5100, shownAs([7, at(leaper.type, 11)])).swings.get(7)?.at.x).toBe(10); // fixed as it began
    const over = 5000 + leaper.attack.windup_ms + leaper.attack.travel_ms + IMPACT_MS;
    expect(a.frame(over - 1, shown).swings.size).toBe(1);
    expect(a.frame(over, shown).swings.size).toBe(0);
  });

  it("a mob's next windup takes its place, and its leaving ends it", () => {
    const a = new MobAttacks(TICK, ground());
    const shown = shownAs([7, at(leaper.type, 10)]);
    a.heard(100, 7, 0, EAST, landing);
    a.frame(5000, shown);
    a.heard(110, 7, 0, -EAST, landing);
    expect(a.frame(5500, shown).swings.get(7)).toMatchObject({ yaw: -EAST, from: 5500 });
    expect(a.frame(5550, shownAs()).swings.size).toBe(0);
    expect(a.frame(5600, shown).swings.size).toBe(0);
  });

  it('a windup for a mob or attack this client does not know shows nothing', () => {
    const a = new MobAttacks(TICK, ground());
    a.heard(100, 7, 5, EAST, landing);
    a.heard(100, 8, 0, EAST, landing);
    const shown = shownAs([7, at(leaper.type, 10)], [8, at('from_a_newer_server', 14)]);
    expect(a.frame(5000, shown).swings.size).toBe(0);
  });

  it('a flinch or knockdown in its windup breaks it off, and a spitter throws nothing', () => {
    for (const pose of [MobPose.FLINCH, MobPose.DOWN]) {
      const a = new MobAttacks(TICK, ground());
      a.heard(100, 7, 0, EAST, landing);
      a.frame(5000, shownAs([7, at(spitter.type, 10)]));
      expect(a.frame(5100, shownAs([7, at(spitter.type, 10, pose)])).swings.size).toBe(0);
      const after = shownAs([7, at(spitter.type, 10)]);
      expect(a.frame(5000 + spitter.attack.windup_ms + 50, after).spits).toEqual([]);
    }
  });

  it('a flinch once the act is under way leaves it shown; a knockdown ends it', () => {
    const a = new MobAttacks(TICK, ground());
    a.heard(100, 7, 0, EAST, landing);
    a.frame(5000, shownAs([7, at(leaper.type, 10)]));
    const travel = 5000 + leaper.attack.windup_ms + 100;
    expect(a.frame(travel, shownAs([7, at(leaper.type, 10, MobPose.FLINCH)])).swings.size).toBe(1);
    expect(a.frame(travel + 50, shownAs([7, at(leaper.type, 10, MobPose.DOWN)])).swings.size).toBe(
      0,
    );
  });

  it('clear forgets every windup and spit', () => {
    const a = new MobAttacks(TICK, ground());
    const shown = shownAs([7, at(spitter.type, 10)]);
    a.heard(100, 7, 0, EAST, landing);
    a.frame(5000 + spitter.attack.windup_ms, shown);
    a.heard(200, 7, 0, EAST, landing);
    a.clear();
    expect(a.frame(10000, shown)).toEqual({ swings: new Map(), spits: [] });
  });

  describe('a spit', () => {
    const { attack, height } = spitter;
    const thrown = 5000 + attack.windup_ms;

    const spitting = (boxes: Box[] = []) => {
      const a = new MobAttacks(TICK, ground(boxes));
      const shown = shownAs([7, at(spitter.type, 10)]);
      a.heard(100, 7, 0, EAST, landing);
      a.frame(5000, shown);
      return { a, shown };
    };

    it("flies from the mob's centre at mid-height along its yaw, at its speed, once the windup ends", () => {
      const { a, shown } = spitting();
      expect(a.frame(thrown - 1, shown).spits).toEqual([]);
      const [s] = a.frame(thrown + 200, shown).spits;
      expect(s).toMatchObject({ mob: 7, y: height / 2, z: 20, size: attack.size });
      expect(s?.x).toBeCloseTo(10 + 0.2 * attack.speed);
    });

    it('ends once it has flown its range', () => {
      const { a, shown } = spitting();
      const ends = thrown + (attack.range / attack.speed) * 1000;
      expect(a.frame(ends - 1, shown).spits).toHaveLength(1);
      expect(a.frame(ends, shown).spits).toEqual([]);
    });

    it('its windup says how far its ball will fly: short of a solid in its way, else its range', () => {
      const wall = { x: 13, z: 20, half_w: 0.25, half_d: 2, ux: 1, uz: 0, top: 3 };
      expect(
        spitting([wall])
          .a.frame(5000, shownAs([7, at(spitter.type, 10)]))
          .swings.get(7)?.reach,
      ).toBeCloseTo(2.75 - attack.size / 2, 1);
      expect(
        spitting()
          .a.frame(5000, shownAs([7, at(spitter.type, 10)]))
          .swings.get(7)?.reach,
      ).toBe(attack.range);
      const leap = new MobAttacks(TICK, ground());
      leap.heard(100, 7, 0, EAST, landing);
      expect(
        leap.frame(5000, shownAs([7, at(leaper.type, 10)])).swings.get(7)?.reach,
      ).toBeUndefined();
    });

    it('ends at a solid in its way, and flies over a low one', () => {
      const wall = { x: 13, z: 20, half_w: 0.25, half_d: 2, ux: 1, uz: 0, top: 3 };
      const { a, shown } = spitting([wall]);
      const at = (d: number) => thrown + (d / attack.speed) * 1000;
      expect(a.frame(at(2.4), shown).spits).toHaveLength(1);
      expect(a.frame(at(2.6), shown).spits).toEqual([]); // its ball meets the wall's west face 2.75 m out, less its radius
      const low = spitting([{ ...wall, top: 0.1 }]);
      expect(low.a.frame(at(4), low.shown).spits).toHaveLength(1);
    });

    it("ends at the mob's Damage, heard while it flies or before it was thrown", () => {
      const { a, shown } = spitting();
      a.frame(thrown + 50, shown);
      a.struckBy(5000 / TICK + attack.windup_ms / TICK + 4, 7);
      expect(a.frame(thrown + 199, shown).spits).toHaveLength(1);
      expect(a.frame(thrown + 200, shown).spits).toEqual([]);
      const b = spitting();
      b.a.struckBy(5000 / TICK + attack.windup_ms / TICK + 2, 7);
      expect(b.a.frame(thrown + 99, b.shown).spits).toHaveLength(1);
      expect(b.a.frame(thrown + 100, b.shown).spits).toEqual([]);
    });

    it('a Damage from another mob, or from before its throw, leaves it flying', () => {
      const { a, shown } = spitting();
      a.struckBy(5000 / TICK + 1, 7);
      a.struckBy(5000 / TICK + attack.windup_ms / TICK + 2, 8);
      expect(a.frame(thrown + 150, shown).spits).toHaveLength(1);
    });

    it('ends as its mob dies or leaves', () => {
      const { a, shown } = spitting();
      a.frame(thrown + 50, shown);
      expect(a.frame(thrown + 100, shownAs([7, { ...at(spitter.type, 10), hp: 0 }])).spits).toEqual(
        [],
      );
      const b = spitting();
      b.a.frame(thrown + 50, b.shown);
      expect(b.a.frame(thrown + 100, shownAs()).spits).toEqual([]);
    });
  });
});
