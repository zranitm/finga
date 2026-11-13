// The drawn looks of the basic attacks and the skills, each sized to its real shape (the box, the range, the area) and run on its beats.
import { classes } from '@/gamedata/classes';
import { movement } from '@/gamedata/movement';
import type { Skill, Stage } from '@/progression/skills';

import type { Beats } from '../entities/beats';

import { colorOf, slashColors } from './colors';
import {
  BODY_MID,
  EDGE,
  animate,
  arcPoints,
  beatsOf,
  brighter,
  flightMs,
  flown,
  jitter,
  middleOf,
  strength,
  stroke,
  through,
  zigzag,
} from './draw';
import type { G, Point } from './draw';
import { attackEffect, skillEffect } from './ids';
import type { EffectId } from './ids';
import type { Body, Drawn, PlayOptions } from './types';

interface Box {
  readonly width: number;
  readonly height: number;
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));
const middle = (o: PlayOptions): Point => ({ x: o.x, y: o.y - BODY_MID });
const casterBody = (o: PlayOptions): Body => ({
  x: o.x,
  y: o.y,
  w: movement.width,
  h: movement.height,
});

interface Swing {
  readonly a0: number; // degrees from straight ahead, below positive
  readonly a1: number;
  readonly reach: number; // the arc's far edge, as a share of the box's width
  readonly width: (lead: number) => number; // along the arc, 1 its leading edge
  readonly color: number;
  readonly inner?: number; // a second, brighter arc inside it, mixed this much toward white
}

/** An arc through the box, about the body's middle, swept out over the strike and faded by the cancel point. */
function swingIn(g: G, o: PlayOptions, box: Box, s: Swing, b: Beats, now: number): void {
  const a = strength(b, now);
  if (a <= 0) {
    return;
  }
  const end = s.a0 + (s.a1 - s.a0) * Math.sqrt(through(b, now)); // fast off the mark
  const c = { x: o.x, y: o.y - box.height / 2 };
  const rx = box.width * s.reach;
  const ry = box.height / 2;
  stroke(g, arcPoints(c, rx, ry, o.facing, s.a0, end), s.width, s.color, a);
  if (s.inner !== undefined) {
    const inner = arcPoints(c, rx * 0.7, ry * 0.7, o.facing, s.a0, end);
    stroke(g, inner, (l) => s.width(l) * 0.5, brighter(s.color, s.inner), a * 0.85);
  }
}

const swing =
  (box: Box, s: Swing): Drawn =>
  (scene, o, now) => {
    const b = beatsOf(o, now);
    return animate(scene, b.cancelAt, (g, t) => swingIn(g, o, box, s, b, t));
  };

/** The Thief's quick thin cut: a straight slash down through the box, gone soon after the strike. */
const cut =
  (box: Box): Drawn =>
  (scene, o, now) => {
    const b = beatsOf(o, now);
    const by = Math.min(b.cancelAt, b.strikeEnd + 80);
    const from = { x: o.x + o.facing * box.width * 0.2, y: o.y - box.height * 0.95 };
    const to = { x: o.x + o.facing * box.width, y: o.y - box.height * 0.1 };
    return animate(scene, by, (g, t) => {
      const k = clamp01(through(b, t) * 2);
      const tip = { x: from.x + (to.x - from.x) * k, y: from.y + (to.y - from.y) * k };
      const a = strength(b, t, by);
      stroke(g, [from, tip], (l) => 1 + 2 * l, slashColors.physical, a);
      stroke(g, [from, tip], 1, 0xffffff, a);
    });
  };

interface Arrow {
  readonly shaft: number;
  readonly width: number;
  readonly color: number;
  readonly trail: number;
}

/** An arrow with its head at head, its trail fading behind it over what it has flown. */
function arrowAt(g: G, head: Point, facing: 1 | -1, flew: number, a: Arrow, alpha = 1): void {
  const trail = Math.min(a.trail, flew);
  for (let i = 0; i < 4; i++) {
    const x0 = head.x - facing * (a.shaft + (trail * i) / 4);
    const x1 = head.x - facing * (a.shaft + (trail * (i + 1)) / 4);
    g.lineStyle(a.width, a.color, alpha * (0.5 - i * 0.12)).lineBetween(x0, head.y, x1, head.y);
  }
  stroke(g, [{ x: head.x - facing * a.shaft, y: head.y }, head], a.width, a.color, alpha);
  const back = head.x - facing * 5;
  g.fillStyle(a.color, alpha).fillTriangle(
    head.x + facing * 3,
    head.y,
    back,
    head.y - 4,
    back,
    head.y + 4,
  );
}

/** A glowing orb with sparkles trailing it. */
function orbAt(g: G, head: Point, facing: 1 | -1, flew: number, now: number): void {
  g.fillStyle(0xb070f0, 0.3).fillCircle(head.x, head.y, 9);
  g.fillStyle(0xf0e0ff, 1).fillCircle(head.x, head.y, 4.5);
  const flicker = Math.floor(now / 60);
  for (let i = 1; i <= 4 && i * 9 < flew; i++) {
    const y = head.y + (jitter(flicker + i) * 2 - 1) * 5;
    g.fillStyle(0xe0c8ff, 0.8 - i * 0.18).fillCircle(head.x - facing * i * 9, y, 2.4 - i * 0.4);
  }
}

/** A shot from the caster's middle flying its range along the facing from the hit, drawn by at with its head where it is. */
const shot =
  (
    range: number,
    at: (g: G, head: Point, o: PlayOptions, flew: number, now: number) => void,
  ): Drawn =>
  (scene, o, now) => {
    const b = beatsOf(o, now);
    const from = middle(o);
    return animate(scene, b.hit + flightMs(range) + 1, (g, t) => {
      const d = flown(b, t, range);
      if (d !== null) {
        at(g, { x: from.x + o.facing * d, y: from.y }, o, d, t);
      }
    });
  };

const BOW: Arrow = { shaft: 14, width: 2, color: 0xf0ead6, trail: 48 };

/** Each class's basic attack, by its id; a class the table lacks swings or shoots like the Beginner or the Bowman. */
function attackLook(c: (typeof classes)[number]): Drawn {
  const a = c.attack;
  if (a.kind === 'ranged') {
    return a.damage === 'magic'
      ? shot(a.range, (g, head, o, flew, now) => orbAt(g, head, o.facing, flew, now))
      : shot(a.range, (g, head, o, flew) => arrowAt(g, head, o.facing, flew, BOW));
  }
  const color = slashColors[a.damage];
  switch (c.id) {
    case 'warrior': // heavy and wide, thicker at its leading edge
      return swing(a, { a0: -80, a1: 80, reach: 1, width: (l) => 2 + 10 * l, color, inner: 0.5 });
    case 'thief':
      return cut(a);
    default: // the Beginner's short swing
      return swing(a, { a0: -50, a1: 50, reach: 0.9, width: (l) => 3 + 3 * l, color });
  }
}

/** The stage a play's tier draws: the skill's own at that tier, or its first. */
const stageOf = (skill: Skill, o: PlayOptions): Stage =>
  skill.stages[o.tier ?? 0] ?? skill.stages[0]!;
const boxOf = (st: Stage): Box => ({ width: st.shape?.width ?? 0, height: st.shape?.height ?? 0 });
const rangeOf = (st: Stage): number => st.shape?.range ?? 0;

/** A drawn version that picks its look from the stage the play's tier draws. */
const byStage =
  (skill: Skill, look: (st: Stage, tier: number) => Drawn): Drawn =>
  (scene, o, now) =>
    look(stageOf(skill, o), o.tier ?? 0)(scene, o, now);

/** Strike: a heavy overhead arc; wider and brighter, then cracking the ground in front. */
function strike(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const box = boxOf(st);
    const base = colorOf(skill, st);
    const s: Swing = {
      a0: -120,
      a1: 45,
      reach: 1,
      width: (l) => 3 + 11 * l,
      color: tier > 0 ? brighter(base, 0.35) : base,
      ...(tier > 0 && { inner: 0.6 }),
    };
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      return animate(scene, b.cancelAt, (g, t) => {
        swingIn(g, o, box, s, b, t);
        if (tier >= 2) {
          crack(g, o, box, b, t);
        }
      });
    };
  });
}

/** A jagged crack along the ground in front, running out over the strike's second half. */
function crack(g: G, o: PlayOptions, box: Box, b: Beats, now: number): void {
  const k = clamp01(through(b, now) * 2 - 1);
  const a = strength(b, now);
  if (k <= 0 || a <= 0) {
    return;
  }
  const y = o.y - 1;
  const from = { x: o.x + o.facing * 10, y };
  const to = { x: from.x + o.facing * (box.width - 10) * k, y };
  const line = zigzag(from, to, 6, 3, 7);
  stroke(g, line, (l) => 4 - 2 * l, 0xffd9a0, a);
  for (let i = 2; i < line.length - 1; i += 2) {
    const p = line[i]!;
    stroke(g, [p, { x: p.x + o.facing * 6, y: p.y - 7 }], 1.5, 0xffd9a0, a * 0.8);
  }
}

/** A stone at c turned by angle: a rough pentagon. */
function stoneAt(g: G, c: Point, r: number, angle: number, alpha: number): void {
  const pts = Array.from({ length: 5 }, (_, i) => {
    const a = angle + (i / 5) * 2 * Math.PI;
    const rr = r * (0.8 + 0.2 * jitter(i + 3));
    return { x: c.x + Math.cos(a) * rr, y: c.y + Math.sin(a) * rr };
  });
  g.fillStyle(0x8a7f70, alpha)
    .lineStyle(1.5, EDGE, alpha * 0.7)
    .beginPath();
  pts.forEach((p, k) => (k === 0 ? g.moveTo(p.x, p.y) : g.lineTo(p.x, p.y)));
  g.closePath().fillPath().strokePath();
}

const VOLLEY_SPREAD = [-14, 0, 14]; // each stone's lane before its foe is known

/** Throw: a spinning stone; bigger with a dust trail; then three, each to a foe hit. */
function throwLook(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const range = rangeOf(st);
    const r = tier > 0 ? 7 : 5;
    const lanes = tier >= 2 ? VOLLEY_SPREAD : [0];
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      const from = middle(o);
      return animate(scene, Math.max(b.cancelAt, b.hit + flightMs(range)), (g, t) => {
        const foes = o.targets?.() ?? [];
        lanes.forEach((lane, i) => {
          const foe = lanes.length > 1 ? foes[i] : undefined;
          const to = foe ? middleOf(foe) : { x: from.x + o.facing * range, y: from.y + lane };
          const dist = Math.max(1, Math.abs(to.x - from.x));
          const d = flown(b, t, dist);
          if (d === null) {
            return;
          }
          const p = d / dist;
          const at = {
            x: from.x + o.facing * d,
            y: from.y + (to.y - from.y) * p - 40 * p * (1 - p),
          };
          if (tier > 0) {
            for (let k = 1; k <= 3 && k * 8 < d; k++) {
              g.fillStyle(0xc8b8a0, 0.45 - k * 0.12).fillCircle(
                at.x - o.facing * k * 8,
                at.y + k,
                2.5 + k,
              );
            }
          }
          stoneAt(g, at, r, (t - b.hit) * 0.025 * o.facing, 1);
        });
      });
    };
  });
}

/** Spark: a short flickering zig-zag bolt; longer and thicker; then lightning leaping between the foes hit. */
function spark(skill: Skill): Drawn {
  return byStage(skill, (st, tier) => {
    const range = rangeOf(st);
    const width = tier > 0 ? 4.5 : 2.5;
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      const from = middle(o);
      return animate(scene, b.cancelAt, (g, t) => {
        const a = strength(b, t);
        if (a <= 0) {
          return;
        }
        const foes = tier >= 2 ? (o.targets?.() ?? []).slice(0, st.targets).map(middleOf) : [];
        const stops =
          foes.length > 0 ? [from, ...foes] : [from, { x: from.x + o.facing * range, y: from.y }];
        const flicker = Math.floor(t / 50);
        for (let i = 1; i < stops.length; i++) {
          const leg = zigzag(
            stops[i - 1]!,
            stops[i]!,
            Math.max(3, Math.round(range / 40)),
            7,
            flicker + i * 5,
          );
          stroke(g, leg, width, 0xb070f0, a);
          stroke(g, leg, width * 0.4, 0xf0e0ff, a);
        }
        for (const f of foes) {
          g.fillStyle(0xf0e0ff, a * 0.8).fillCircle(f.x, f.y, 5);
        }
      });
    };
  });
}

/** Two quick thrusts, one after the other, each a flash along the box at its own height. */
function doubleStab(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const box = boxOf(st);
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      const half = (b.strikeEnd - b.hit) / 2;
      return animate(scene, b.cancelAt, (g, t) => {
        [0.62, 0.38].forEach((h, i) => {
          const from = b.hit + i * half;
          const k = (t - from) / half;
          const a = k < 1 ? 1 : clamp01(1 - (t - from - half) / 120);
          if (k < 0 || a <= 0) {
            return;
          }
          const y = o.y - box.height * h;
          const x0 = o.x + o.facing * 8;
          const tip = { x: x0 + o.facing * (box.width - 8) * Math.min(1, k * 1.5), y };
          stroke(g, [{ x: x0, y }, tip], (l) => 1.5 + 2.5 * l, slashColors.physical, a);
          g.fillStyle(0xffffff, a).fillCircle(tip.x, tip.y, 3);
        });
      });
    };
  });
}

const BURST_MS = 220;

/** Fire Arrow: a burning arrow that bursts on the first foe it hits; one that hits nothing flies its range. */
function fireArrow(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const range = rangeOf(st);
    const head: Arrow = { shaft: 16, width: 3, color: 0xff7a2a, trail: 60 };
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      const from = middle(o);
      let seen: number | null = null; // when its foe was first known: a hit of the player's own comes a round trip late
      return animate(scene, Math.max(b.cancelAt, b.hit + flightMs(range)) + BURST_MS, (g, t) => {
        const foe = o.targets?.()[0];
        seen ??= foe ? t : null;
        const to = foe ? Math.min(range, Math.abs(foe.x - from.x)) : range;
        const d = flown(b, t, to);
        if (d !== null && !(foe && seen !== null && d >= to)) {
          const at = { x: from.x + o.facing * d, y: from.y };
          arrowAt(g, at, o.facing, d, head);
          const flicker = Math.floor(t / 40);
          for (let k = 1; k <= 3; k++) {
            const y = at.y + (jitter(flicker + k) * 2 - 1) * 4;
            g.fillStyle(0xffc040, 0.7 - k * 0.18).fillCircle(
              at.x - o.facing * k * 7,
              y,
              3.5 - k * 0.6,
            );
          }
          return;
        }
        if (foe && seen !== null) {
          const age = t - Math.max(b.hit + flightMs(to), seen);
          if (age >= 0 && age < BURST_MS) {
            const k = age / BURST_MS;
            const m = middleOf(foe);
            g.lineStyle(4, 0xff7a2a, 1 - k).strokeCircle(m.x, m.y, 6 + 20 * k);
            g.fillStyle(0xffd070, (1 - k) * 0.8).fillCircle(m.x, m.y, 8 * (1 - k) + 2);
          }
        }
      });
    };
  });
}

/** A four-point sparkle. */
function sparkleAt(g: G, x: number, y: number, r: number, color: number, alpha: number): void {
  g.lineStyle(1.5, color, alpha).lineBetween(x - r, y, x + r, y);
  g.lineBetween(x, y - r, x, y + r);
  g.fillStyle(0xffffff, alpha).fillCircle(x, y, r * 0.35);
}

/** Heal: sparkles rising over each player it heals, from their feet to over their head. */
function heal(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const color = colorOf(skill, st);
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      return animate(scene, b.cancelAt, (g, t) => {
        const k = clamp01((t - b.hit) / Math.max(1, b.cancelAt - b.hit));
        const fade = k < 0.7 ? 1 : (1 - k) / 0.3;
        for (const body of o.targets?.() ?? [casterBody(o)]) {
          for (let i = 0; i < 6; i++) {
            const rise = (k + i / 6) % 1;
            const x = body.x + (jitter(i + 1) - 0.5) * body.w * 1.4;
            const y = body.y - rise * (body.h + 16);
            sparkleAt(g, x, y, 3 + 2 * jitter(i + 7), color, Math.sin(rise * Math.PI) * fade);
          }
        }
      });
    };
  });
}

const AURA_MS = 600;

/** A buff: a short aura in its colour around the caster, swelling and fading, as wide as its area. */
function aura(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    const r = st.shape?.radius ?? movement.width;
    const color = colorOf(skill, st);
    return (scene, o, now) => {
      const b = beatsOf(o, now);
      const until = Math.min(b.cancelAt, b.hit + AURA_MS);
      return animate(scene, until, (g, t) => {
        const k = clamp01((t - b.hit) / Math.max(1, until - b.hit));
        const m = middleOf(o.follow?.() ?? casterBody(o));
        const s = 0.75 + 0.35 * k;
        const a = (1 - k) * 0.9;
        g.fillStyle(color, a * 0.15).fillEllipse(m.x, m.y, 2 * r * s, 1.5 * r * s);
        g.lineStyle(7, EDGE, a * 0.35).strokeEllipse(m.x, m.y, 2 * r * s, 1.5 * r * s);
        g.lineStyle(3, color, a).strokeEllipse(m.x, m.y, 2 * r * s, 1.5 * r * s);
      });
    };
  });
}

/** A skill this table lacks still shows by its shape's kind: a swing, an arrow, or an aura. */
function byShape(skill: Skill): Drawn {
  return byStage(skill, (st) => {
    switch (st.shape?.kind) {
      case 'melee':
        return swing(boxOf(st), {
          a0: -60,
          a1: 60,
          reach: 1,
          width: (l) => 3 + 6 * l,
          color: colorOf(skill, st),
        });
      case 'ranged':
        return shot(rangeOf(st), (g, head, o, flew) =>
          arrowAt(g, head, o.facing, flew, { ...BOW, color: colorOf(skill, st) }),
        );
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
      swing(boxOf(st), {
        a0: -85,
        a1: 85,
        reach: 1,
        width: (l) => 4 + 10 * l,
        color: colorOf(skill, st),
        inner: 0.4,
      }),
    ),
  double_stab: doubleStab,
  power_shot: (skill) =>
    byStage(skill, (st) =>
      shot(rangeOf(st), (g, head, o, flew) =>
        arrowAt(g, head, o.facing, flew, { shaft: 22, width: 4, color: 0xfff2c0, trail: 150 }),
      ),
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
