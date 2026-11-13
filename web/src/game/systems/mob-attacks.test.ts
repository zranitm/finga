import { describe, expect, it } from 'vitest';

import { mobs } from '@/gamedata/mobs';
import { MobPose } from '@/protocol/messages';

import type { MobState } from './combat-view';
import { MobAttacks } from './mob-attacks';
import type { MobAttack } from './mob-attacks';

const TICK = 50;

/** The first mob whose first attack is of kind. */
function mobWith(kind: MobAttack['kind']) {
  const m = mobs.find((x) => x.attacks[0]?.kind === kind);
  if (!m) {
    throw new Error(`no mob in the data attacks first with a ${kind}`);
  }
  return { type: m.id, width: m.width, height: m.height, attack: m.attacks[0] };
}

const leaper = mobWith('leap');
const spitter = mobWith('shot');

const at = (type: string, x: number, facing: 1 | -1 = 1, pose = MobPose.UNSPECIFIED): MobState => ({
  type,
  x,
  y: 608,
  facing,
  hp: 10,
  maxHp: 10,
  pose,
  poseMs: 0,
});

const shownAs = (...mobsShown: [number, MobState][]) => new Map(mobsShown);

describe('an attack drawn through its phases', () => {
  /** The pose a windup heard at tick 100, at 5000 ms, draws at t on a fresh MobAttacks. */
  const drawnAt = (m: typeof leaper, t: number) => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 0, 1);
    return a.frame(t, shownAs([7, at(m.type, 300)])).poses.get(7)?.pose;
  };
  const act = 5000 + leaper.attack.windup_ms;
  const lands = act + leaper.attack.travel_ms;

  it.each([
    { t: 4999, want: undefined },
    { t: 5000, want: { phase: 'windup', progress: 0 } },
    { t: 5000 + leaper.attack.windup_ms / 2, want: { phase: 'windup', progress: 0.5 } },
    { t: act, want: { phase: 'travel', progress: 0 } },
    { t: act + leaper.attack.travel_ms / 4, want: { phase: 'travel', progress: 0.25 } },
    { t: lands, want: { phase: 'impact', progress: 0 } },
    { t: lands + 1000, want: undefined },
  ])('a hop at $t ms', ({ t, want }) => {
    expect(drawnAt(leaper, t)).toEqual(want);
  });

  it('the impact plays out after the landing', () => {
    const p = drawnAt(leaper, lands + 50);
    expect(p?.phase).toBe('impact');
    expect(p?.progress).toBeGreaterThan(0);
    expect(p?.progress).toBeLessThan(1);
  });

  it('an attack with no travel goes from its windup straight to its impact', () => {
    expect(spitter.attack.travel_ms).toBe(0);
    expect(drawnAt(spitter, 5000 + spitter.attack.windup_ms)).toEqual({
      phase: 'impact',
      progress: 0,
    });
  });
});

describe('MobAttacks', () => {
  it("draws a windup once the render clock reaches its tick, so it lines up with the mob's drawn place", () => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 0, -1);
    const shown = shownAs([7, at(leaper.type, 300)]);
    expect(a.frame(4999, shown).poses.size).toBe(0);
    expect(a.frame(5000, shown).poses.get(7)).toEqual({
      swing: { attack: leaper.attack, facing: -1, from: 5000 },
      pose: { phase: 'windup', progress: 0 },
    });
    const lands = 5000 + leaper.attack.windup_ms + leaper.attack.travel_ms;
    expect(a.frame(lands, shown).poses.get(7)?.pose.phase).toBe('impact');
    expect(a.frame(lands + 1000, shown).poses.size).toBe(0);
  });

  it("a mob's next windup ends what it showed, and its leaving ends it too", () => {
    const a = new MobAttacks(TICK, []);
    const shown = shownAs([7, at(leaper.type, 300)]);
    a.heard(100, 7, 0, 1);
    a.frame(5000, shown);
    a.heard(110, 7, 0, -1);
    expect(a.frame(5500, shown).poses.get(7)?.swing).toMatchObject({ facing: -1, from: 5500 });
    expect(a.frame(5550, shownAs()).poses.size).toBe(0);
    expect(a.frame(5600, shown).poses.size).toBe(0);
  });

  it('a windup for a mob or attack this client does not know draws nothing', () => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 5, 1);
    a.heard(100, 8, 0, 1);
    const drawn = a.frame(
      5000,
      shownAs([7, at(leaper.type, 300)], [8, at('from_a_newer_server', 400)]),
    );
    expect(drawn.poses.size).toBe(0);
  });

  it('a flinch or a knockdown in its windup ends the drawing, and a spitter throws nothing', () => {
    for (const pose of [MobPose.FLINCH, MobPose.DOWN]) {
      const a = new MobAttacks(TICK, []);
      a.heard(100, 7, 0, 1);
      a.frame(5000, shownAs([7, at(spitter.type, 300)]));
      expect(a.frame(5100, shownAs([7, at(spitter.type, 300, 1, pose)])).poses.size).toBe(0);
      const after = shownAs([7, at(spitter.type, 300)]);
      expect(a.frame(5000 + spitter.attack.windup_ms + 50, after)).toEqual({
        poses: new Map(),
        spits: [],
      });
    }
  });

  it('a flinch once the act is under way leaves it drawn', () => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 0, 1);
    a.frame(5000, shownAs([7, at(leaper.type, 300)]));
    const flight = 5000 + leaper.attack.windup_ms + 100;
    expect(
      a.frame(flight, shownAs([7, at(leaper.type, 300, 1, MobPose.FLINCH)])).poses.get(7)?.pose
        .phase,
    ).toBe('travel');
  });

  it('a knockdown once the act is under way ends it, though a spit already thrown flies on', () => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 0, 1);
    a.frame(5000, shownAs([7, at(leaper.type, 300)]));
    const flight = 5000 + leaper.attack.windup_ms + 100;
    expect(a.frame(flight, shownAs([7, at(leaper.type, 300, 1, MobPose.DOWN)])).poses.size).toBe(0);

    const b = new MobAttacks(TICK, []);
    b.heard(100, 7, 0, 1);
    b.frame(5000, shownAs([7, at(spitter.type, 300)]));
    const thrown = 5000 + spitter.attack.windup_ms;
    const down = shownAs([7, { ...at(spitter.type, 300, 1, MobPose.DOWN), poseMs: 50 }]); // down since 50 ms after the throw
    const drawn = b.frame(thrown + 100, down);
    expect([drawn.poses.size, drawn.spits.length]).toEqual([0, 1]);
    expect(b.frame(thrown + 200, down).spits).toHaveLength(1);
  });

  it('a frame past the throw that finds a flinch from the windup throws nothing', () => {
    const a = new MobAttacks(TICK, []);
    a.heard(100, 7, 0, 1);
    a.frame(5000, shownAs([7, at(spitter.type, 300)]));
    const thrown = 5000 + spitter.attack.windup_ms;
    const flinched = shownAs([7, { ...at(spitter.type, 300, 1, MobPose.FLINCH), poseMs: 250 }]); // since 150 ms before the throw
    expect(a.frame(thrown + 100, flinched)).toEqual({ poses: new Map(), spits: [] });
  });

  it('clear forgets every windup and spit', () => {
    const a = new MobAttacks(TICK, []);
    const shown = shownAs([7, at(spitter.type, 300)]);
    a.heard(100, 7, 0, 1);
    a.frame(5000 + spitter.attack.windup_ms, shown);
    a.heard(200, 7, 0, 1);
    a.clear();
    expect(a.frame(10000, shown)).toEqual({ poses: new Map(), spits: [] });
  });

  describe('a spit', () => {
    const { attack, width, height } = spitter;
    const thrown = 5000 + attack.windup_ms;
    const front = 300 + width / 2;

    const spitting = (solids: { x: number; y: number; w: number; h: number }[] = []) => {
      const a = new MobAttacks(TICK, solids);
      const shown = shownAs([7, at(spitter.type, 300)]);
      a.heard(100, 7, 0, 1);
      a.frame(5000, shown);
      return { a, shown };
    };

    it("flies from the mob's front at mid-height, at its speed, once the windup ends", () => {
      const { a, shown } = spitting();
      expect(a.frame(thrown - 1, shown).spits).toEqual([]);
      const [s] = a.frame(thrown + 200, shown).spits;
      expect(s).toMatchObject({
        mob: 7,
        type: spitter.type,
        y: 608 - height / 2,
        size: attack.size,
      });
      expect(s?.x).toBeCloseTo(front + 0.2 * attack.speed + attack.size / 2);
    });

    it("ends once its front is its range past the mob's", () => {
      const { a, shown } = spitting();
      const ends = thrown + ((attack.range - attack.size) / attack.speed) * 1000;
      const [last] = a.frame(ends - 1, shown).spits;
      expect((last?.x ?? 0) + attack.size / 2).toBeLessThan(front + attack.range);
      expect((last?.x ?? 0) + attack.size / 2).toBeGreaterThan(front + attack.range - 1);
      expect(a.frame(ends, shown).spits).toEqual([]);
    });

    it('ends at a solid in its lane', () => {
      const wall = { x: front + 100, y: 400, w: 32, h: 300 };
      const { a, shown } = spitting([wall]);
      const ends = thrown + ((100 - attack.size) / attack.speed) * 1000;
      expect(a.frame(ends - 1, shown).spits).toHaveLength(1);
      expect(a.frame(ends, shown).spits).toEqual([]);
    });

    it('flies past a solid above its lane', () => {
      const ledge = { x: front + 100, y: 400, w: 32, h: 100 };
      const { a, shown } = spitting([ledge]);
      expect(a.frame(thrown + (150 / attack.speed) * 1000, shown).spits).toHaveLength(1);
    });

    it("ends at the mob's Damage, heard while it flies", () => {
      const { a, shown } = spitting();
      a.frame(thrown + 50, shown);
      a.struckBy(5000 / TICK + attack.windup_ms / TICK + 4, 7);
      expect(a.frame(thrown + 199, shown).spits).toHaveLength(1);
      expect(a.frame(thrown + 200, shown).spits).toEqual([]);
    });

    it('ends at a Damage heard before the render clock threw it', () => {
      const { a, shown } = spitting();
      a.struckBy(5000 / TICK + attack.windup_ms / TICK + 2, 7);
      expect(a.frame(thrown + 99, shown).spits).toHaveLength(1);
      expect(a.frame(thrown + 100, shown).spits).toEqual([]);
    });

    it('a Damage from another mob, or from before its throw, leaves it flying', () => {
      const { a, shown } = spitting();
      a.struckBy(5000 / TICK + 1, 7);
      a.struckBy(5000 / TICK + attack.windup_ms / TICK + 2, 8);
      expect(a.frame(thrown + 150, shown).spits).toHaveLength(1);
    });

    it('goes with its mob', () => {
      const { a, shown } = spitting();
      a.frame(thrown + 50, shown);
      expect(a.frame(thrown + 100, shownAs()).spits).toEqual([]);
    });

    it('ends as its mob dies, and a mob killed in its windup throws none', () => {
      const dead = shownAs([7, { ...at(spitter.type, 300), hp: 0 }]);
      const { a, shown } = spitting();
      a.frame(thrown + 50, shown);
      expect(a.frame(thrown + 100, dead).spits).toEqual([]);
      const b = spitting().a;
      expect(b.frame(thrown - 50, dead).poses.size).toBe(0);
      expect(b.frame(thrown + 50, dead)).toEqual({ poses: new Map(), spits: [] });
    });

    it('flies left from a mob facing left', () => {
      const a = new MobAttacks(TICK, []);
      const shown = shownAs([7, at(spitter.type, 300, -1)]);
      a.heard(100, 7, 0, -1);
      a.frame(5000, shown);
      const [s] = a.frame(thrown + 200, shown).spits;
      expect(s?.x).toBeCloseTo(300 - width / 2 - 0.2 * attack.speed - attack.size / 2);
    });
  });
});
