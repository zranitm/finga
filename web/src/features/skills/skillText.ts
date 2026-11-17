// What the skill window says a skill does, by its effect, from the numbers the server uses.
import { itemStats, statText } from '@/components/item-stats';
import { scaledAt, skillCooldownTicks } from '@/progression/skills';
import type { Inflict, Scaled, Skill, Stage } from '@/progression/skills';
import { statusName, statusOf } from '@/progression/statuses';

/** Two decimals at most, so 1.55 stays 1.55 and 2 reads 2. */
function num(n: number): string {
  return String(Math.round(n * 100) / 100);
}

/** A bonus to a stat in the units items use, as an item card reads it. */
function bonusText(stat: string, value: number): string {
  const info = itemStats.find((s) => s.data === stat);
  return info ? `${info.label} +${statText(info, value)}` : `${stat} +${num(value)}`;
}

/** A stage's bonuses at points, each stat whole in its units as the server rounds them. */
function bonusesAt(bonus: Readonly<Record<string, Scaled>>, points: number): string[] {
  return Object.entries(bonus).map(([stat, s]) => bonusText(stat, Math.round(scaledAt(s, points))));
}

/** "a", "a and b", "a, b and c". */
function and(items: readonly string[]): string {
  return items.length < 2 ? items.join('') : `${items.slice(0, -1).join(', ')} and ${items.at(-1)}`;
}

/** A sentence's first letter in capitals. */
function cap(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** A status as a skill puts it on: its name and time, and damage over time's tick. */
function inflictText(i: Inflict, points: number): string {
  const time = i.time_ms > 0 ? ` ${num(i.time_ms / 1000)}s` : '';
  const every = statusOf(i.status)?.every_ms ?? 0;
  const tick =
    every > 0 ? `, ${num(scaledAt(i.power, points))}× attack every ${num(every / 1000)}s` : '';
  return `${statusName(i.status)}${time}${tick}`;
}

/** What a damage or status skill does besides its damage: its status and its push. */
function onFoes(stage: Stage, points: number): string[] {
  return [
    ...(stage.status ? [inflictText(stage.status, points)] : []),
    ...(stage.push > 0 ? [`push ${num(stage.push)} m`] : []),
  ];
}

/** What a status's power adds with each point, for damage over time. */
function inflictPerPoint(i: Inflict | null): string[] {
  return i && i.power.per_point > 0 ? [`${statusName(i.status)} +${num(i.power.per_point)}×`] : [];
}

/** Who a heal reaches, as the world picks them: the caster if hurt, then the nearest hurt party members in its shape, up to its targets. */
function healReach(stage: Stage): string {
  const within = stage.shape?.radius ? ` within ${stage.shape.radius}` : '';
  return stage.targets > 1
    ? `you and the nearest hurt party members${within}, up to ${stage.targets} in all`
    : `you if hurt, else the nearest hurt party member${within}`;
}

/** What a cooldown runs at: the character's cooldown speed, in the world's ticks. */
export interface CooldownClock {
  readonly speed: number;
  readonly tickMs: number;
}

/** What the stage does with points invested; its cooldown is the one the skill bar runs. */
export function effectText(
  skill: Skill,
  stage: Stage,
  points: number,
  clock: CooldownClock,
): string {
  if (skill.kind === 'passive') {
    return bonusesAt(stage.bonus, points).join(' · ');
  }
  const cooldownMs =
    skillCooldownTicks(stage.cooldown_ms, clock.speed, clock.tickMs) * clock.tickMs;
  const cost = [`${stage.energy} energy`, `${num(cooldownMs / 1000)}s cooldown`];
  const power = num(scaledAt(stage.power, points));
  switch (skill.effect) {
    case 'buff': {
      // A buff is on the caster alone, so its shape reaches no one else.
      const lasts = `for ${num(stage.duration_ms / 1000)}s`;
      const bonuses = bonusesAt(stage.bonus, points);
      return [
        ...(bonuses.length > 0 ? [`${and(bonuses)} on you ${lasts}`] : []),
        ...(stage.on_hit
          ? [
              `${bonuses.length > 0 ? 'y' : 'Y'}our hits inflict ${inflictText(stage.on_hit, points)} ${lasts}`,
            ]
          : []),
        ...cost,
      ].join(' · ');
    }
    case 'heal':
      return [`Heals ${healReach(stage)}, by ${power}× magic attack`, ...cost].join(' · ');
    case 'status': {
      const parts = [`${cap(and(onFoes(stage, points)))} on foes`, ...cost];
      if (stage.shape?.range) parts.push(`range ${stage.shape.range}`);
      if (stage.shape?.radius) parts.push(`radius ${stage.shape.radius}`);
      return parts.join(' · ');
    }
    default: {
      const parts = [
        `${power}× ${stage.damage === 'magic' ? 'magic' : 'physical'} attack`,
        ...onFoes(stage, points),
        ...cost,
      ];
      if (stage.shape?.range) parts.push(`range ${stage.shape.range}`);
      if (stage.shape?.radius) parts.push(`radius ${stage.shape.radius}`);
      if (stage.targets > 1) parts.push(`up to ${stage.targets} foes`);
      return parts.join(' · ');
    }
  }
}

/** How long an active stage takes, and whether a basic attack can cut it short. */
export function actionText(stage: Stage): string {
  const takes = `Takes ${num(stage.length_ms / 1000)}s.`;
  return stage.cancelable ? `${takes} A basic attack can cut it short.` : takes;
}

/** What each point adds in the stage; the energy it costs stays the same. */
export function perPointText(skill: Skill, stage: Stage): string {
  const bonuses = Object.entries(stage.bonus).map(([stat, s]) => bonusText(stat, s.per_point));
  if (skill.kind === 'passive' || skill.effect === 'buff') {
    return [...bonuses, ...inflictPerPoint(stage.on_hit)].join(' · ');
  }
  if (skill.effect === 'status') {
    return inflictPerPoint(stage.status).join(' · ');
  }
  return skill.effect === 'heal'
    ? `heal +${num(stage.power.per_point)}× magic attack`
    : [`power +${num(stage.power.per_point)}×`, ...inflictPerPoint(stage.status)].join(' · ');
}
