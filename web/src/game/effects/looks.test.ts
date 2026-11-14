import type Phaser from 'phaser';
import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';
import { skills } from '@/gamedata/skills';
import { actionPoints } from '@/progression/actions';
import { skillById } from '@/progression/skills';
import type { Stage } from '@/progression/skills';
import { recordingScene } from '@/testing/recording-scene';
import type { Call } from '@/testing/recording-scene';

import { DEPTH } from '../depth';
import { beatsOf } from '../entities/beats';
import type { Beats } from '../entities/beats';
import { attackTiming, skillTiming } from '../systems/actions';

import { colorOf } from './colors';
import { EDGE, flightMs } from './draw';
import { attackEffect, skillEffect } from './ids';
import type { EffectId } from './ids';
import { shotRange } from './looks';
import { play } from './play';
import type { Body, PlayOptions } from './types';

const TICK = 50;
const START = 1000;
const X = 300;
const FEET = 608;
const MIDDLE = FEET - 28;

interface Point {
  x: number;
  y: number;
}

/** The points a call draws through: a line's ends, a circle's or an ellipse's extremes, a triangle's corners. */
function pointsOf(c: Call): Point[] {
  const n = c.slice(1).map(Number);
  const [a = 0, b = 0, w = 0, h = 0, e = 0, f = 0] = n;
  switch (c[0]) {
    case 'graphics.lineBetween':
      return [
        { x: a, y: b },
        { x: w, y: h },
      ];
    case 'graphics.moveTo':
    case 'graphics.lineTo':
      return [{ x: a, y: b }];
    case 'graphics.fillCircle':
    case 'graphics.strokeCircle':
      return [
        { x: a - w, y: b },
        { x: a + w, y: b },
        { x: a, y: b - w },
        { x: a, y: b + w },
      ];
    case 'graphics.fillEllipse':
    case 'graphics.strokeEllipse':
      return [
        { x: a - w / 2, y: b - h / 2 },
        { x: a + w / 2, y: b + h / 2 },
      ];
    case 'graphics.fillTriangle':
      return [
        { x: a, y: b },
        { x: w, y: h },
        { x: e, y: f },
      ];
    default:
      return [];
  }
}

/** Plays id from START and records what each frame draws, every 10 ms till `to`. */
function run(id: EffectId, beats: Beats, to: number, o: Partial<PlayOptions> = {}) {
  const s = recordingScene();
  play(s.scene, id, { x: X, y: FEET, facing: 1, beats, ...o });
  const frames = new Map<number, Point[]>();
  const calls = new Map<number, Call[]>();
  let seen = s.calls.length;
  for (let t = START; t <= to; t += 10) {
    s.frame(t);
    const drew = s.calls.slice(seen);
    seen = s.calls.length;
    calls.set(t, drew);
    frames.set(t, drew.flatMap(pointsOf));
  }
  const all = [...frames.values()].flat();
  return { frames, calls, all, running: s.running };
}

const attackBeats = (cls: string) => beatsOf(attackTiming(undefined, cls, TICK), START, TICK);
const skillBeats = (st: Stage) => beatsOf(skillTiming(st, TICK), START, TICK);
const near = (pts: readonly Point[], to: Point, px: number) =>
  pts.some((p) => Math.hypot(p.x - to.x, p.y - to.y) <= px);

/** Each look, with its beats, how far its shape reaches in front (a box's width, a range, an area's radius) and whether it flies there. */
const plays = [
  ...classes.map((c) => ({
    id: attackEffect(c.id),
    tier: 0,
    beats: attackBeats(c.id),
    reach: c.attack.kind === 'ranged' ? c.attack.range : c.attack.width,
    flies: c.attack.kind === 'ranged',
  })),
  ...skills
    .filter((s) => s.kind === 'active')
    .flatMap((s) =>
      skillById(s.id)!.stages.map((st, tier) => ({
        id: skillEffect(s.id),
        tier,
        beats: skillBeats(st),
        reach: st.shape?.width ?? st.shape?.range ?? st.shape?.radius ?? 0,
        flies: st.shape?.kind === 'ranged',
      })),
    ),
];

describe('looks', () => {
  it.each(plays.filter((p) => p.id !== 'skill.heal'))(
    '$id at tier $tier draws from its strike, as far as its shape reaches either way, and is gone by its ready point or once it has flown',
    ({ id, tier, beats, reach, flies }) => {
      const gone = flies ? Math.max(beats.readyAt, beats.hit + flightMs(reach) + 1) : beats.readyAt;
      for (const facing of [1, -1] as const) {
        const { frames, all, running } = run(id, beats, START + 2000, { facing, tier });
        const striking = [...frames].filter(([t]) => t >= beats.hit && t < beats.strikeEnd);
        expect(striking.some(([, pts]) => pts.length > 0)).toBe(true);
        const ahead = Math.max(...all.map((p) => (p.x - X) * facing));
        expect(ahead).toBeGreaterThanOrEqual(0.6 * reach);
        expect(ahead).toBeLessThanOrEqual(reach + 14);
        for (const [t, pts] of frames) {
          if (t >= gone) {
            expect(pts, `${t}`).toEqual([]);
          }
        }
        expect(running()).toBe(0);
      }
    },
  );

  it('an attack’s slash still shows past its cancel point, just after its strike, as it times to its ready point', () => {
    const t = attackTiming(undefined, 'warrior', TICK);
    const beats = beatsOf(t, START, TICK);
    const cancelAt = START + actionPoints(t.ticks, t.hit, t.cancel).cancel * TICK; // 250 ms in, ready at 750
    const { frames } = run('attack.warrior', beats, beats.readyAt);
    expect([...frames].some(([at, pts]) => at > cancelAt && pts.length > 0)).toBe(true);
  });

  const foes: Body[] = [
    { x: 460, y: 620, w: 32, h: 28 },
    { x: 540, y: 590, w: 40, h: 32 },
  ];

  it.each(['skill.strike', 'skill.throw', 'skill.spark'] as const)(
    '%s looks different at each evolution, hitting the same foes',
    (id) => {
      const st = skillById(id.slice('skill.'.length))!.stages;
      const drawn = st.map((stage, tier) => {
        const { calls } = run(id, skillBeats(stage), START + 200, { tier, targets: () => foes });
        return JSON.stringify([calls.get(START + 60), calls.get(START + 140)]);
      });
      expect(new Set(drawn).size).toBe(3);
    },
  );
  const mid = (b: Body) => ({ x: b.x, y: b.y - b.h / 2 });

  it("Arc's lightning leaps to each foe hit, and toward the facing before they are known", () => {
    const st = skillById('spark')!.stages[2]!;
    const hit = run('skill.spark', skillBeats(st), START + 140, { tier: 2, targets: () => foes });
    expect(foes.every((f) => near(hit.all, mid(f), 1))).toBe(true);
    const blind = run('skill.spark', skillBeats(st), START + 140, { tier: 2, targets: () => [] });
    expect(near(blind.all, { x: X + st.shape!.range!, y: MIDDLE }, 1)).toBe(true);
  });

  it("Volley's three stones each fly to a foe hit; before they are known, fanned toward the facing", () => {
    const st = skillById('throw')!.stages[2]!;
    const hit = run('skill.throw', skillBeats(st), START + 450, { tier: 2, targets: () => foes });
    expect(foes.every((f) => near(hit.all, mid(f), 14))).toBe(true);
    const blind = run('skill.throw', skillBeats(st), START + 450, { tier: 2 });
    const end = X + st.shape!.range!;
    expect([-14, 0, 14].every((lane) => near(blind.all, { x: end, y: MIDDLE + lane }, 14))).toBe(
      true,
    );
  });

  it('a skill drawn as a flying shot says how far it flies, so its hits wait for it; Spark, Arc and melee skills strike at once', () => {
    const range = (id: string, tier = 0) => {
      const s = skillById(id)!;
      return shotRange(s, s.stages[tier]!);
    };
    expect([range('throw'), range('throw', 2), range('power_shot'), range('fire_arrow')]).toEqual([
      300, 380, 460, 380,
    ]);
    expect([range('spark'), range('spark', 2), range('strike'), range('heal')]).toEqual([
      undefined,
      undefined,
      undefined,
      undefined,
    ]);
    const unlisted = { ...skillById('power_shot')!, id: 'a_new_shot' }; // no look of its own: drawn by its ranged shape
    expect(shotRange(unlisted, unlisted.stages[0]!)).toBe(460);
    const swung = { ...skillById('strike')!, id: 'a_new_swing' };
    expect(shotRange(swung, swung.stages[0]!)).toBeUndefined();
  });

  it('Fire Arrow bursts on the first foe it hits, even one known after it flew past', () => {
    const st = skillById('fire_arrow')!.stages[0]!;
    const burst = (known: number) => {
      const s = recordingScene();
      let t = START;
      play(s.scene, 'skill.fire_arrow', {
        x: X,
        y: FEET,
        facing: 1,
        beats: skillBeats(st),
        targets: () => (t >= known ? foes.slice(0, 1) : []),
      });
      for (; t <= START + 900; t += 10) {
        s.frame(t);
      }
      return s.named('graphics.strokeCircle').map(([x, y]) => ({ x: Number(x), y: Number(y) }));
    };
    expect(near(burst(START), mid(foes[0]!), 0.5)).toBe(true);
    expect(near(burst(START + 300), mid(foes[0]!), 0.5)).toBe(true); // a round trip late
    expect(burst(START + 5000)).toEqual([]); // it hit nothing
  });

  it('Heal sparkles rise over each player it heals; buffs swell an aura around their caster wherever they walk', () => {
    const st = skillById('heal')!.stages[0]!;
    const healed: Body[] = [
      { x: 300, y: 608, w: 24, h: 56 },
      { x: 420, y: 560, w: 24, h: 56 },
    ];
    const { all } = run('skill.heal', skillBeats(st), START + 1500, { targets: () => healed });
    for (const b of healed) {
      const over = all.filter((p) => Math.abs(p.x - b.x) <= b.w);
      expect(Math.min(...over.map((p) => p.y))).toBeLessThan(b.y - b.h);
    }
    const iron = skillById('iron_body')!.stages[0]!;
    const s = recordingScene();
    let x = 300;
    play(s.scene, 'skill.iron_body', {
      x: 300,
      y: FEET,
      facing: 1,
      beats: skillBeats(iron),
      follow: () => ({ x, y: FEET, w: 24, h: 56 }),
    });
    x = 340;
    s.frame(START + 200);
    expect(s.named('graphics.strokeEllipse').at(-1)?.slice(0, 2)).toEqual([340, MIDDLE]);
  });

  const cutOff = {
    x: 300,
    y: FEET,
    texture: { key: 'alienBlue_hit' },
    frame: { name: '__BASE' },
    originX: 0.5,
    originY: 1,
    scaleX: 0.3,
    scaleY: 0.2,
    angle: 15,
    flipX: false,
  } as unknown as Phaser.GameObjects.Sprite;

  it("a cancel's afterimage is the pose cut off, in white with a dark rim, fading from 0.8 to nothing over 300 ms as it drifts 14 px back", () => {
    const s = recordingScene();
    play(s.scene, 'cancel.afterimage', { x: 300, y: FEET, facing: 1, copy: cutOff });
    expect(s.named('add.sprite')).toEqual([[300, FEET, 'alienBlue_hit', '__BASE']]);
    expect([
      s.named('sprite.setOrigin'),
      s.named('sprite.setScale'),
      s.named('sprite.setAngle'),
      s.named('sprite.setFlipX'),
      s.named('sprite.setTint'),
      s.named('sprite.setTintMode'),
      s.named('sprite.setDepth'),
    ]).toEqual([
      [[0.5, 1]],
      [[0.3, 0.2]],
      [[15]],
      [[false]],
      [[0xffffff]],
      [[1]],
      [[DEPTH.afterimage]],
    ]);
    // The rim: a dark glow 3 px out and none inward, so it never covers the fill; padded so the frame's edge doesn't cut it.
    expect(s.named('sprite.filters.external.addGlow')).toEqual([[EDGE, 4, 0, 1, false, 8, 3]]);
    expect(s.named('sprite.filters.external.setPaddingOverride')).toEqual([[null]]);
    s.frame(START + 150);
    expect(s.named('sprite.filterCamera.setAlpha').map(([a]) => a)).toEqual([0.8, 0.4]); // rim and fill fade as one
    expect(s.named('sprite.setAlpha')).toEqual([]); // the fill stays whole under the filter, else the rim would outlast it
    expect(s.named('sprite.setX').at(-1)).toEqual([293]);
    s.frame(START + 300);
    expect([s.named('sprite.destroy').length, s.running()]).toEqual([1, 0]); // the rim is the sprite's filter, gone with it
  });

  it('without WebGL the afterimage has no rim, and the sprite itself fades', () => {
    const s = recordingScene({ webgl: false });
    play(s.scene, 'cancel.afterimage', { x: 300, y: FEET, facing: -1, copy: cutOff });
    s.frame(START + 150);
    expect(s.named('sprite.setAlpha').map(([a]) => a)).toEqual([0.8, 0.4]);
    expect(s.named('sprite.setX').at(-1)).toEqual([307]);
    s.frame(START + 300);
    expect([s.named('sprite.destroy').length, s.running()]).toEqual([1, 0]);
  });

  it("a skill's colour is its damage type's, or a heal's or a buff's own", () => {
    const at = (id: string) => colorOf(skillById(id)!, skillById(id)!.stages[0]!);
    expect([at('strike'), at('spark'), at('heal'), at('haste')]).toEqual([
      0xff6a3d, 0xb070f0, 0x4fd8ff, 0xf0b23a,
    ]);
  });
});
