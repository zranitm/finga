// Mirrors server/internal/progression's skills for the skill window and the character window's preview; testdata/stats pins passives to the server's.
import { classes } from '@/gamedata/classes';
import { skills as data } from '@/gamedata/skills';

import { lineOf } from './classes';
import { derivedStats, primaryKeys } from './stats';
import type { Roll } from './stats';

export interface Scaled {
  readonly base: number;
  readonly per_point: number;
}

/** Where an active skill finds its targets: a box in front, along the facing, or around the caster. */
export interface Shape {
  readonly kind: string;
  readonly width?: number;
  readonly height?: number;
  readonly range?: number;
  readonly radius?: number;
}

/** A skill from at points on, every field resolved from the stages before it; bonus is in the units items use. */
export interface Stage {
  readonly at: number;
  readonly name: string;
  readonly description: string;
  readonly damage: string | null;
  readonly shape: Shape | null;
  readonly mp: Scaled;
  readonly cooldown_ms: number;
  readonly power: Scaled;
  readonly targets: number;
  readonly duration_ms: number;
  readonly bonus: Readonly<Record<string, Scaled>>;
}

export interface Skill {
  readonly id: string;
  readonly kind: string;
  readonly effect: string;
  /** The class that teaches it. */
  readonly class: string;
  /** The first at 0 points, the totals rising. */
  readonly stages: readonly Stage[];
}

const skills: readonly Skill[] = data;

/** A value with points invested. */
export function scaledAt(s: Scaled, points: number): number {
  return s.base + s.per_point * points;
}

/** The skill with id, if this client knows it. */
export function skillById(id: string): Skill | undefined {
  return skills.find((s) => s.id === id);
}

/** The index of the stage a skill is in with points: the last whose total they reach. */
export function stageAt(skill: Skill, points: number): number {
  let k = 0;
  skill.stages.forEach((st, i) => {
    if (points >= st.at) k = i;
  });
  return k;
}

/** Every skill a class can put points in: its own, then each class's above it, up to the Beginner's. */
export function learnable(cls: string): Skill[] {
  return lineOf(cls).flatMap((id) =>
    (classes.find((c) => c.id === id)?.skills ?? []).flatMap((s) => skillById(s) ?? []),
  );
}

const keyOf = new Map<string, keyof Roll>([
  ...primaryKeys.map((k) => [k, k] as const),
  ...derivedStats.map((s) => [s.data, s.key] as const),
]);

/** A bonus with points invested, each stat rounded to a whole number in the units items use, as an item's roll is. */
export function bonusAt(bonus: Readonly<Record<string, Scaled>>, points: number): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  for (const [stat, s] of Object.entries(bonus)) {
    const key = keyOf.get(stat);
    if (key !== undefined) out[key] = Math.round(scaledAt(s, points));
  }
  return out;
}

/** What the passive skills a class can learn add, each at its stage and points; one with none adds nothing. */
export function passives(cls: string, invested: Readonly<Record<string, number>>): Roll {
  const out: Partial<Record<keyof Roll, number>> = {};
  for (const skill of learnable(cls)) {
    const points = invested[skill.id] ?? 0;
    if (skill.kind !== 'passive' || points < 1) continue;
    for (const [k, v] of Object.entries(
      bonusAt(skill.stages[stageAt(skill, points)]!.bonus, points),
    )) {
      const key = k as keyof Roll;
      out[key] = (out[key] ?? 0) + (v ?? 0);
    }
  }
  return out;
}

/** What an active stage costs with points invested: a whole number of MP, rounded. */
export function mpAt(stage: Stage, points: number): number {
  return Math.round(scaledAt(stage.mp, points));
}
