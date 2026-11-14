import { describe, expect, it } from 'vitest';

import { recordingScene } from '@/testing/recording-scene';

import type { Beats } from '../entities/beats';

import { effectIds } from './ids';
import type { EffectId } from './ids';
import { play } from './play';
import type { Body, PlayOptions } from './types';

const START = 1000;

/** A windup of 600 ms, a travel of travel ms, then the 250 ms impact. */
const beats = (travel = 0): Beats => {
  const hit = START + 600;
  const strikeEnd = hit + travel;
  return {
    start: START,
    hit,
    strikeEnd,
    readyAt: strikeEnd + 250,
    end: strikeEnd + 250,
  };
};

/** Plays a mob attack's look at a 32 × 28 mob, and what each frame drew at the times given. */
function frames(id: EffectId, o: Partial<PlayOptions>, at: readonly number[]) {
  const s = recordingScene();
  let mob: Body = { x: 500, y: 608, w: 32, h: 28 };
  play(s.scene, id, { x: 500, y: 608, facing: 1, size: { w: 32, h: 28 }, follow: () => mob, ...o });
  const drew = at.map((t) => {
    s.calls.length = 0;
    s.frame(t);
    return [...s.calls];
  });
  return { drew, running: s.running, move: (b: Body) => (mob = b) };
}

const named = (calls: readonly (readonly unknown[])[], name: string) =>
  calls.filter((c) => c[0] === `graphics.${name}`).map((c) => c.slice(1).map(Number));

describe('mob attack looks', () => {
  const mobIds = effectIds.filter((id) => id.startsWith('mob.'));

  it('names every attack in mobs.yaml', () => {
    expect([...mobIds].sort()).toEqual([
      'mob.bite',
      'mob.charge',
      'mob.hop',
      'mob.slam',
      'mob.spit',
    ]);
  });

  it.each(mobIds)(
    '%s rings the mob through its windup as it moves, and is gone by its end',
    (id) => {
      const s = recordingScene();
      let x = 500;
      play(s.scene, id, {
        x: 500,
        y: 608,
        facing: 1,
        beats: beats(400),
        follow: () => ({ x, y: 608, w: 32, h: 28 }),
      });
      x = 520;
      s.calls.length = 0;
      s.frame(START + 300);
      expect(named(s.calls, 'strokeEllipse')).toEqual([[520, 594, 40, 36]]);
      s.frame(START + 600 + 400 + 250);
      expect(s.running()).toBe(0);
    },
  );

  it("a bite's arc, a hop's landing and a slam's ring reach as far as the attack does", () => {
    const at = START + 600; // the impact's start, for an attack with no travel
    const bite = frames('mob.bite', { beats: beats(), reach: 24 }, [at]).drew[0]!;
    expect(named(bite, 'arc')).toEqual([[516, 594, 26, -Math.PI / 2, Math.PI / 2]]); // its front, (28 + 24) / 2 across
    const hop = frames('mob.hop', { beats: beats(), reach: 12 }, [at]).drew[0]!;
    expect(named(hop, 'strokeEllipse')).toEqual([[500, 608, (32 + 24) * 0.6, 10]]);
    const slam = frames('mob.slam', { beats: beats(), reach: 48 }, [at + 250 - 1]).drew[0]!;
    const [, , w = 0, h = 0] = named(slam, 'strokeEllipse')[0] ?? [];
    expect([w, h]).toEqual([expect.closeTo(32 + 96, 0), expect.closeTo(28 + 96, 0)]);
  });

  it.each([1, -1] as const)('a charge facing %d trails dust behind it as it travels', (facing) => {
    const drew = frames('mob.charge', { beats: beats(400), facing }, [START + 800]).drew[0]!;
    const dust = named(drew, 'fillCircle').map(([x = 0]) => Math.sign(x - 500));
    expect(dust).toEqual([-facing, -facing, -facing]);
  });

  it("a hop a portal's edge stopped short rings where its hit came down; one that landed where it aimed, at its body as before", () => {
    const landing = START + 600 + 400; // the impact's start, after 400 ms in the air
    const clipped = frames('mob.hop', { beats: beats(400), reach: 12, to: 560 }, [landing]); // its body held at 500
    expect(named(clipped.drew[0]!, 'strokeEllipse')).toEqual([[560, 608, (32 + 24) * 0.6, 10]]);
    const aimed = frames('mob.hop', { beats: beats(400), reach: 12, to: 500 }, [landing]);
    expect(named(aimed.drew[0]!, 'strokeEllipse')).toEqual([[500, 608, (32 + 24) * 0.6, 10]]);
  });

  it("a charge's dust follows its hit on past a body a portal's edge stopped", () => {
    // Its body held at 500, its hit going 128 px on to 628 over 400 ms: half way at 800, three quarters at 900.
    const { drew } = frames('mob.charge', { beats: beats(400), to: 628 }, [
      START + 800,
      START + 900,
    ]);
    const dust = drew.map((d) => named(d, 'fillCircle').map(([x = 0]) => x));
    expect(dust).toEqual([
      [564 - 24, 564 - 32, 564 - 40],
      [596 - 24, 596 - 32, 596 - 40],
    ]);
  });

  it.each([1, -1] as const)(
    "a charge facing %d stops its hit at the first player it meets, as the server's does, guarded or not",
    (facing) => {
      // One player behind it and one ahead, whose near side is 68 px on: its front stops there, its body's centre 52 px on.
      const players = () => [
        { x: 500 - facing * 50, y: 608, w: 24, h: 56 },
        { x: 500 + facing * 80, y: 608, w: 24, h: 56 },
      ];
      const { drew } = frames(
        'mob.charge',
        { beats: beats(400), facing, to: 500 + facing * 128, players },
        [START + 800, START + 900, START + 950],
      );
      const lead = drew.map((d) => (named(d, 'fillCircle')[0]?.[0] ?? 0) + facing * 24);
      expect(lead).toEqual([500 + facing * 52, 500 + facing * 52, 500 + facing * 52]);
    },
  );

  it("a spit's balls fly in the mob's colour, or the spit's own green on an untinted mob", () => {
    const ball: Body = { x: 600, y: 600, w: 12, h: 12 };
    const flying = { ...beats(), end: START + 1700 }; // a spit's look lasts its ball's flight past the impact
    const tinted = frames('mob.spit', { beats: flying, shots: () => [ball], color: 0x9a9ab0 }, [
      START + 900,
    ]).drew[0]!;
    const fills = (calls: readonly (readonly unknown[])[]) =>
      calls.filter((c) => c[0] === 'graphics.fillStyle').map((c) => c[1]);
    expect(named(tinted, 'fillCircle')).toEqual([[600, 594, 6]]);
    expect(fills(tinted)).toEqual([0x9a9ab0]);
    const plain = frames('mob.spit', { beats: flying, shots: () => [ball] }, [START + 900])
      .drew[0]!;
    expect(fills(plain)).toEqual([0xc8e6a0]);
  });
});
