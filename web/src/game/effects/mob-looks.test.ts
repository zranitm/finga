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
    cancelAt: strikeEnd + 250,
    end: strikeEnd + 250,
    slows: false,
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
