import { readFileSync } from 'node:fs';
import path from 'node:path';

import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { position, ViewExpander, whole } from './expand';
import {
  DeltaSchema,
  MobStepSchema,
  MobViewSchema,
  PlayerStepSchema,
  PlayerViewSchema,
} from './messages';

// npm runs the scripts from web/; the vectors are the server's too (quant_test.go).
const vectors = JSON.parse(
  readFileSync(path.resolve(process.cwd(), '..', 'server', 'testdata', 'quant.json'), 'utf8'),
) as {
  position: { metres: number; cm: number }[];
  speed: { metresPerSecond: number; mm: number }[];
  heading: { unit: number; milli: number }[];
  yaw: { radians: number; milli: number }[];
};

describe('rounding', () => {
  it('agrees with the server on every vector', () => {
    for (const c of vectors.position) {
      expect(position(c.metres)).toBe(c.cm);
    }
    for (const c of vectors.speed) {
      expect(whole(c.metresPerSecond * 1000)).toBe(c.mm);
    }
    for (const c of vectors.heading) {
      expect(whole(c.unit * 1000)).toBe(c.milli);
    }
    for (const c of vectors.yaw) {
      expect(whole(Math.fround(c.radians) * 1000)).toBe(c.milli);
    }
  });
});

describe('ViewExpander', () => {
  const ayla = create(PlayerViewSchema, { id: 'a', slot: 1, x: 10.004, z: 5.5, hp: 100 });
  const robin = create(PlayerViewSchema, { id: 'r', slot: 2, x: 20.126, z: 6, vx: 1, hp: 50 });
  const slime = create(MobViewSchema, { id: 9, x: 3.333, z: 4, hp: 34, yaw: 1 });

  it('moves a held view by whole centimetres from where its place rounds to', () => {
    const e = new ViewExpander('a');
    e.snapshot([ayla, robin], [slime]);
    const d = create(DeltaSchema, {
      steps: [create(PlayerStepSchema, { slot: 2, dx: -13, dz: 4, vx: 2500, hp: 45 })],
      mobSteps: [create(MobStepSchema, { id: 9, dx: 1, dz: -2, yaw: -1813, hp: 20 })],
    });
    const { players, mobs } = e.delta(d);
    expect(players).toHaveLength(1);
    expect(players[0]).toMatchObject({ id: 'r', hp: 45, vx: 2.5 });
    expect(players[0]?.x).toBeCloseTo(20, 10); // 2013 cm less 13
    expect(players[0]?.z).toBeCloseTo(6.04, 10);
    expect(mobs[0]?.x).toBeCloseTo(3.34, 10);
    expect(mobs[0]?.hp).toBe(20);
    expect(mobs[0]?.yaw).toBeCloseTo(-1.813, 5);
  });

  it('leaves out its own steps, and a body that went', () => {
    const e = new ViewExpander('a');
    e.snapshot([ayla, robin], [slime]);
    const step = (slot: number) => create(PlayerStepSchema, { slot, dx: 1 });
    expect(e.delta(create(DeltaSchema, { steps: [step(1)] })).players).toHaveLength(0);
    e.delta(create(DeltaSchema, { removedPlayers: ['r'] }));
    expect(e.delta(create(DeltaSchema, { steps: [step(2)] })).players).toHaveLength(0);
  });

  it('takes a whole view as the last word of a frame on a body also sent as a step', () => {
    const e = new ViewExpander('a');
    e.snapshot([ayla, robin], [slime]);
    const { players, mobs } = e.delta(
      create(DeltaSchema, {
        players: [create(PlayerViewSchema, { ...robin, x: 20.136 })],
        steps: [create(PlayerStepSchema, { slot: 2, dx: 10 })],
        mobs: [create(MobViewSchema, { ...slime, x: 3.343 })],
        mobSteps: [create(MobStepSchema, { id: 9, dx: 10 })],
      }),
    );
    expect(players[players.length - 1]?.x).toBeCloseTo(20.136, 10);
    expect(mobs[mobs.length - 1]?.x).toBeCloseTo(3.343, 10);
    const next = e.delta(
      create(DeltaSchema, { steps: [create(PlayerStepSchema, { slot: 2, dx: 5 })] }),
    );
    expect(next.players[0]?.x).toBeCloseTo(20.19, 10); // 2014 cm and 5, from the whole view
  });
});
