// The drawn looks of the basic attacks and the skills (combat-effects.md section 5), each sized to its real shape (the arc, the range, the area) and run on its beats.
import { Vector3 } from '@babylonjs/core/Maths/math.vector';
import type { TransformNode } from '@babylonjs/core/Meshes/transformNode';

import { classes } from '@/gamedata/classes';
import type { Skill, Stage } from '@/progression/skills';

import { brighter, colorOf, slashColors } from './colors';
import {
  along,
  animate,
  arrow,
  ball,
  Band,
  beatsOf,
  CHEST_M,
  clamp01,
  Line,
  ring,
  stone,
  strength,
  through,
  zigzag,
} from './draw';
import { flightMs, flown } from './flight';
import type { Fx } from './fx';
import { attackEffect, damageType, skillEffect } from './ids';
import type { EffectId } from './ids';
import { burst, stream } from './particles';
import type { Body, Drawn, PlayOptions, Point } from './types';

const DEG = Math.PI / 180;
const AURA_MS = 600;
const BURST_MS = 220;

/** Where the caster is now, or where they stood as the play began. */
const feetOf = (o: PlayOptions): Point => o.follow?.() ?? o;

/** A body's middle. */
const middleOf = (b: Body): Point => ({ x: b.x, y: b.y + b.height / 2, z: b.z });

/** A swing: a band from `from` to `to` degrees off the aim (right positive), its outer side at `reach` of the shape's radius, `width` metres across along it (1 its leading end), rolled about the aim, centred `up` metres over the feet. */
interface Swing {
  readonly from: number;
  readonly to: number;
  readonly reach: number;
  readonly width: (lead: number) => number;
  readonly color: number;
  readonly roll: number;
  readonly up: number;
  readonly inner?: number; // a second, brighter band inside it, mixed this much toward white
  readonly fadeBy?: (b: { readonly strikeEnd: number; readonly readyAt: number }) => number;
}

/** A swing drawn out over the strike, fast off the mark, following its caster, faded by the ready point. */
function swing(radius: number, s: Swing): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const by = s.fadeBy?.(b) ?? b.readyAt;
    const bands = [
      new Band(fx, s.color),
      ...(s.inner !== undefined ? [new Band(fx, brighter(s.color, s.inner))] : []),
    ];
    const r = radius * s.reach;
    return animate(fx, now, by, bands, (t) => {
      const a = strength(b, t, by);
      const end = s.from + (s.to - s.from) * Math.sqrt(through(b, t));
      const at = feetOf(o);
      bands.forEach((band, i) => {
        band.root.position.set(at.x, at.y + s.up, at.z);
        band.root.rotation.set(0, o.yaw, s.roll * DEG);
        const k = i === 0 ? 1 : 0.7;
        band.draw(s.from * DEG, end * DEG, r * k, (l) => s.width(l) * k, i === 0 ? a : a * 0.85);
      });
    });
  };
}

/** A shot's head: what flies, placed at its point along the aim. */
interface Head {
  readonly root: TransformNode;
  dispose(): void;
}

/** A shot from the caster's chest along the aim from its hit at a shot's pace, up to range; each frame `at` places it, `flew` metres out. */
function shot(
  range: number,
  make: (fx: Fx, from: Point, yaw: number) => Head,
  step?: (h: Head, t: number, flew: number) => void,
): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const from = along(o, o.yaw, 0.3, 0, CHEST_M);
    const h = make(fx, from, o.yaw);
    h.root.setEnabled(false);
    return animate(fx, now, b.hit + flightMs(range) + 1, [h], (t) => {
      const d = flown(b, t, range);
      h.root.setEnabled(d !== null);
      if (d !== null) {
        const p = along(from, o.yaw, d);
        h.root.position.set(p.x, p.y, p.z);
        step?.(h, t, d);
      }
    });
  };
}

interface ArrowSpec {
  readonly shaft: number;
  readonly width: number;
  readonly color: number;
  readonly trail: number;
}

const BOW: ArrowSpec = { shaft: 0.45, width: 0.04, color: 0xf0ead6, trail: 1.5 };

function arrowAt(a: ArrowSpec): (fx: Fx, from: Point, yaw: number) => Head {
  return (fx, from, yaw) => {
    const h = arrow(fx, a, from);
    h.root.rotation.y = yaw;
    return h;
  };
}

/** A glowing orb with sparkles streaming off it. */
function orb(fx: Fx, from: Point): Head {
  const glow = ball(fx, 0xb070f0, 0.34);
  const core = ball(fx, 0xf0e0ff, 0.16);
  core.parent = glow;
  glow.position.set(from.x, from.y, from.z);
  const sparkles = stream(fx, glow, {
    rate: 50,
    color: 0xe0c8ff,
    size: [0.15, 0.25],
    life: [0.15, 0.3],
    speed: [0.2, 0.5],
  });
  return {
    root: glow,
    dispose: () => {
      sparkles();
      glow.dispose();
    },
  };
}

/** Each class's basic attack, by its id; a class the table lacks swings or shoots like the Beginner or the Bowman. */
function attackLook(c: (typeof classes)[number]): Drawn {
  const a = c.attack;
  if (a.kind === 'shot') {
    return a.damage === 'magic' ? shot(a.range, orb) : shot(a.range, arrowAt(BOW));
  }
  const color = slashColors[damageType(a.damage)];
  switch (c.id) {
    case 'warrior': // heavy and wide, thicker at its leading edge
      return swing(a.radius, {
        from: 80,
        to: -80,
        reach: 0.9,
        width: (l) => 0.08 + 0.42 * l,
        color,
        roll: 12,
        up: 1.05,
        inner: 0.5,
      });
    case 'thief': // a quick thin cut down across the front, gone soon after the strike
      return swing(a.radius, {
        from: 45,
        to: -50,
        reach: 0.8,
        width: (l) => 0.03 + 0.1 * l,
        color,
        roll: 55,
        up: 1.1,
        fadeBy: (b) => Math.min(b.readyAt, b.strikeEnd + 80),
      });
    default: // the Beginner's short swing
      return swing(a.radius, {
        from: 50,
        to: -50,
        reach: 0.75,
        width: (l) => 0.08 + 0.16 * l,
        color,
        roll: 20,
        up: 1,
      });
  }
}

/** The stage a play's tier draws: the skill's own at that tier, or its first. */
const stageOf = (skill: Skill, o: PlayOptions): Stage =>
  skill.stages[o.tier ?? 0] ?? skill.stages[0]!;
const radiusOf = (st: Stage): number => st.shape?.radius ?? 0;
const rangeOf = (st: Stage): number => st.shape?.range ?? 0;

/** A drawn version that picks its look from the stage the play's tier draws. */
const byStage =
  (skill: Skill, look: (st: Stage, tier: number) => Drawn): Drawn =>
  (fx, o, now) =>
    look(stageOf(skill, o), o.tier ?? 0)(fx, o, now);

/** Strike: a heavy overhead arc; wider and brighter, then cracking the ground ahead. */
function strike(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const base = colorOf(skill, st);
    const arc = swing(radiusOf(st), {
      from: 110,
      to: -35,
      reach: tier > 0 ? 0.62 : 0.55,
      width: (l) => 0.1 + (tier > 0 ? 0.5 : 0.38) * l,
      color: tier > 0 ? brighter(base, 0.35) : base,
      roll: 90,
      up: 0.75,
      ...(tier > 0 && { inner: 0.6 }),
    });
    if (tier < 2) {
      return arc;
    }
    return (fx, o, now) => {
      const stopArc = arc(fx, o, now);
      const stopCrack = crack(radiusOf(st))(fx, o, now);
      return () => {
        stopArc();
        stopCrack();
      };
    };
  });
}

/** A jagged crack along the ground ahead, running out over the strike's second half. */
function crack(radius: number): Drawn {
  return (fx, o, now) => {
    const b = beatsOf(o, now);
    const kinks = 6;
    const line = new Line(fx, 0xffd9a0, kinks + 2, 0.04);
    return animate(fx, now, b.readyAt, [line], (t) => {
      const k = clamp01(through(b, t) * 2 - 1);
      const from = along(o, o.yaw, 0.5, 0, 0.03);
      const to = along(o, o.yaw, 0.5 + (radius - 0.5) * Math.max(0.01, k), 0, 0.03);
      const jag = zigzag(from, to, kinks, 0.12, 7).map((p) => ({ ...p, y: from.y }));
      line.draw(jag, k > 0 ? strength(b, t) : 0);
    });
  };
}

const VOLLEY_SPREAD = [-12, 0, 12]; // each stone's lane in degrees before its foe is known

/** Throw: a spinning stone thrown in an arc; bigger with a dust trail; then three, each to a foe hit. */
function throwLook(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const range = rangeOf(st);
    const size = tier > 0 ? 0.13 : 0.09;
    const lanes = tier >= 2 ? VOLLEY_SPREAD : [0];
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const from = along(o, o.yaw, 0.3, 0, CHEST_M);
      const stones = lanes.map(() => {
        const s = stone(fx, size);
        s.position.set(from.x, from.y, from.z);
        s.setEnabled(false);
        const dust =
          tier > 0
            ? stream(fx, s, {
                rate: 40,
                color: 0xc8b8a0,
                size: [0.25, 0.4],
                life: [0.2, 0.3],
                speed: [0.1, 0.3],
                blend: 'over',
              })
            : null;
        return { s, dispose: () => (dust?.(), s.dispose()) };
      });
      return animate(fx, now, Math.max(b.readyAt, b.hit + flightMs(range)), stones, (t) => {
        const foes = o.targets?.() ?? [];
        lanes.forEach((lane, i) => {
          const { s } = stones[i]!;
          const foe = lanes.length > 1 ? foes[i] : undefined;
          const to = foe ? middleOf(foe) : along(from, o.yaw + lane * DEG, range);
          const dist = Math.max(0.1, Math.hypot(to.x - from.x, to.z - from.z));
          const d = flown(b, t, dist);
          s.setEnabled(d !== null);
          if (d === null) {
            return;
          }
          const p = d / dist;
          s.position.set(
            from.x + (to.x - from.x) * p,
            from.y + (to.y - from.y) * p + 1.2 * p * (1 - p),
            from.z + (to.z - from.z) * p,
          );
          s.rotation.set((t - b.hit) * 0.02, (t - b.hit) * 0.013, 0);
        });
      });
    };
  });
}

/** Spark: a short flickering zig-zag bolt; longer and thicker; then lightning leaping between the foes hit. */
function spark(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const range = rangeOf(st);
    const width = tier > 0 ? 0.07 : 0.04;
    const legs = tier >= 2 ? Math.max(1, st.targets) : 1;
    const kinks = Math.max(3, Math.round(range / 1.25));
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const lines = Array.from({ length: legs }, () => [
        new Line(fx, 0xb070f0, kinks + 2, width),
        new Line(fx, 0xf0e0ff, kinks + 2, width * 0.4),
      ]).flat();
      return animate(fx, now, b.readyAt, lines, (t) => {
        const a = strength(b, t);
        const from = along(feetOf(o), o.yaw, 0.3, 0, CHEST_M);
        const foes = tier >= 2 ? (o.targets?.() ?? []).slice(0, legs).map(middleOf) : [];
        const stops = foes.length > 0 ? [from, ...foes] : [from, along(from, o.yaw, range)];
        const flicker = Math.floor(t / 50);
        for (let i = 0; i < legs; i++) {
          const [outer, inner] = [lines[2 * i]!, lines[2 * i + 1]!];
          const a0 = stops[i];
          const a1 = stops[i + 1];
          if (!a0 || !a1) {
            outer.draw([], 0);
            inner.draw([], 0);
            continue;
          }
          const leg = zigzag(a0, a1, kinks, 0.22, flicker + i * 5);
          outer.draw(leg, a);
          inner.draw(leg, a);
        }
      });
    };
  });
}

/** Two quick thrusts, one after the other, each a flash along the aim at its own height. */
function doubleStab(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const radius = radiusOf(st);
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const half = (b.strikeEnd - b.hit) / 2;
      const lines = [0, 1].map(() => new Line(fx, slashColors.physical, 2, 0.05));
      return animate(fx, now, b.readyAt, lines, (t) => {
        const at = feetOf(o);
        [1.25, 0.95].forEach((h, i) => {
          const from = b.hit + i * half;
          const k = (t - from) / half;
          const a = k < 1 ? 1 : clamp01(1 - (t - from - half) / 120);
          const reach = 0.3 + (radius - 0.3) * clamp01(k * 1.5);
          lines[i]!.draw(
            [along(at, o.yaw, 0.3, 0, h), along(at, o.yaw, reach, 0, h)],
            k < 0 ? 0 : a,
          );
        });
      });
    };
  });
}

/** Fire Arrow: a burning arrow that bursts on the first foe it hits; one that hits nothing flies its range. */
function fireArrow(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const range = rangeOf(st);
    const head: ArrowSpec = { shaft: 0.5, width: 0.06, color: 0xff7a2a, trail: 1.8 };
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const from = along(o, o.yaw, 0.3, 0, CHEST_M);
      const h = arrow(fx, head, from);
      h.root.rotation.y = o.yaw;
      const flame = stream(fx, h.head, {
        rate: 60,
        color: 0xffc040,
        size: [0.2, 0.35],
        life: [0.1, 0.2],
        speed: [0.1, 0.4],
      });
      let burstAt: number | null = null;
      const parts = [{ dispose: () => (flame(), h.dispose()) }];
      return animate(
        fx,
        now,
        Math.max(b.readyAt, b.hit + flightMs(range)) + BURST_MS,
        parts,
        (t) => {
          const foe = o.targets?.()[0];
          const to = foe ? Math.min(range, Math.hypot(foe.x - from.x, foe.z - from.z)) : range;
          const d = flown(b, t, to);
          const landed = foe !== undefined && t >= b.hit + flightMs(to);
          h.root.setEnabled(d !== null && !landed);
          if (d !== null && !landed) {
            const p = along(from, o.yaw, d);
            h.root.position.set(p.x, p.y, p.z);
          }
          if (landed && foe && burstAt === null) {
            burstAt = t;
            parts.push({
              dispose: burst(fx, middleOf(foe), {
                count: 30,
                color: 0xffd070,
                fade: 0xff5a10,
                size: [0.3, 0.5],
                speed: [2, 4],
                life: [0.15, 0.22],
                blend: 'add',
              }),
            });
          }
        },
      );
    };
  });
}

/** Heal: sparkles rising over each player it heals, from their feet to over their head. */
function heal(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const color = colorOf(skill, st);
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const shown: { dispose(): void }[] = [];
      const streams: { at: Vector3; stop: () => void }[] = [];
      return animate(fx, now, b.readyAt, shown, (t) => {
        const bodies = o.targets?.() ?? [feetOf(o) as Body];
        bodies.forEach((body, i) => {
          let s = streams[i];
          if (!s && t >= b.hit) {
            const at = new Vector3(body.x, body.y, body.z);
            const stop = rising(fx, at, color);
            s = { at, stop };
            streams.push(s);
            shown.push({ dispose: stop });
          }
          s?.at.set(body.x, body.y, body.z);
        });
      });
    };
  });
}

/** Sparkles rising from feet at `at`, which may move, to over the head, till stopped. */
function rising(fx: Fx, at: Vector3, color: number): () => void {
  return stream(fx, at, {
    rate: 30,
    color,
    size: [0.25, 0.4],
    life: [0.5, 0.7],
    speed: [0, 0.2],
    rise: 3,
    spread: 0.4,
    blend: 'add',
  });
}

/** A buff: a short aura in its colour round the caster, swelling and fading, as wide as its area. */
function aura(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const r = Math.max(0.6, radiusOf(st) * 0.6);
    const color = colorOf(skill, st);
    return (fx, o, now) => {
      const b = beatsOf(o, now);
      const until = Math.min(b.readyAt, b.hit + AURA_MS);
      const rings = [ring(fx, color, 0.08), ring(fx, color, 0.05)];
      return animate(fx, now, until, rings, (t) => {
        const k = clamp01((t - b.hit) / Math.max(1, until - b.hit));
        const at = feetOf(o);
        rings.forEach((m, i) => {
          const s = r * (0.75 + 0.35 * k) * (i === 0 ? 1 : 0.8);
          m.position.set(at.x, at.y + 0.1 + i * 0.8 + k * 0.3, at.z);
          m.scaling.set(s, 1, s);
          m.visibility = t < b.hit ? 0 : (1 - k) * 0.9;
        });
      });
    };
  });
}

/** A skill this table lacks still shows by its shape's kind: a swing, an arrow, or an aura. */
function byShape(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    switch (st.shape?.kind) {
      case 'arc':
        return swing(radiusOf(st), {
          from: 60,
          to: -60,
          reach: 0.85,
          width: (l) => 0.1 + 0.25 * l,
          color: colorOf(skill, st),
          roll: 15,
          up: 1,
        });
      case 'shot':
        return shot(rangeOf(st), arrowAt({ ...BOW, color: colorOf(skill, st) }));
      default:
        return skill.effect === 'heal' ? heal(skill) : aura(skill);
    }
  });
}

const skillLooks: Readonly<Record<string, (skill: Skill) => Drawn>> = {
  strike,
  throw: throwLook,
  spark,
  slash_blast: (skill) =>
    byStage(skill, (st) =>
      swing(radiusOf(st), {
        from: 90,
        to: -90,
        reach: 0.9,
        width: (l) => 0.12 + 0.5 * l,
        color: colorOf(skill, st),
        roll: 0,
        up: 0.95,
        inner: 0.4,
      }),
    ),
  double_stab: doubleStab,
  power_shot: (skill) =>
    byStage(skill, (st) =>
      shot(rangeOf(st), arrowAt({ shaft: 0.7, width: 0.08, color: 0xfff2c0, trail: 4 })),
    ),
  fire_arrow: fireArrow,
  heal,
  iron_body: aura,
  haste: aura,
  focus: aura,
};

/** Every class's attack and every active skill's look, by id. */
export function looks(skills: readonly Skill[]): [EffectId, Drawn][] {
  return [
    ...classes.map((c): [EffectId, Drawn] => [attackEffect(c.id), attackLook(c)]),
    ...skills.map((s): [EffectId, Drawn] => [skillEffect(s.id), (skillLooks[s.id] ?? byShape)(s)]),
  ];
}
