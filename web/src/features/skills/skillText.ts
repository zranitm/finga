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

/** How a stage moves its user and how long it dodges: "dashes up to 4 m at the locked foe", "dodging for 0.4s". */
function moveText(stage: Stage): string[] {
  const m = stage.move;
  const out: string[] = [];
  if (m) {
    const verb = m.kind === 'blink' ? 'blinks' : m.kind === 'leap' ? 'leaps' : 'dashes';
    const way =
      m.toward === 'lock'
        ? m.kind === 'blink'
          ? 'behind the locked foe'
          : 'at the locked foe'
        : m.toward === 'back'
          ? 'back'
          : 'the way you walk, or back if standing';
    out.push(`${verb} ${m.toward === 'back' ? '' : 'up to '}${num(m.distance)} m ${way}`);
  }
  if (stage.dodge_ms > 0) {
    out.push(`dodging for ${num(stage.dodge_ms / 1000)}s`);
  }
  return out;
}

/** Where a stage's shape reaches: its range, or its radius, round the locked foe for an area at the lock. */
function reachText(stage: Stage): string[] {
  const sh = stage.shape;
  if (sh?.range) {
    return [`range ${sh.range}`];
  }
  if (!sh?.radius) {
    return [];
  }
  return sh.at === 'lock'
    ? [`radius ${sh.radius} round the locked foe within ${num(sh.reach ?? 0)} m`]
    : [`radius ${sh.radius}`];
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

/** What the stage does with points invested; its cooldown is the one the skill bar runs. A break-free says first that it goes only while held. */
export function effectText(
  skill: Skill,
  stage: Stage,
  points: number,
  clock: CooldownClock,
): string {
  const text = effectsOf(skill, stage, points, clock);
  return skill.breaks_control
    ? `Only while held: ends every hard status on you · ${text.charAt(0).toLowerCase()}${text.slice(1)}`
    : text;
}

function effectsOf(skill: Skill, stage: Stage, points: number, clock: CooldownClock): string {
  if (skill.kind === 'passive') {
    return bonusesAt(stage.bonus, points).join(' · ');
  }
  const cooldownMs =
    skillCooldownTicks(stage.cooldown_ms, clock.speed, clock.tickMs) * clock.tickMs;
  const cost = [`${stage.energy} energy`, `${num(cooldownMs / 1000)}s cooldown`];
  const power = num(scaledAt(stage.power, points));
  switch (skill.effect) {
    case 'buff': {
      // A buff is on the caster alone, unless it reaches the party within its radius.
      const lasts = `for ${num(stage.duration_ms / 1000)}s`;
      const on = stage.reach_party
        ? `on you and your party within ${num(stage.shape?.radius ?? 0)} m`
        : 'on you';
      const bonuses = bonusesAt(stage.bonus, points);
      return [
        ...(bonuses.length > 0 ? [`${and(bonuses)} ${on} ${lasts}`] : []),
        ...(stage.shield
          ? [
              `${bonuses.length > 0 ? 'a' : 'A'} shield of ${num(scaledAt(stage.shield.power, points))}× magic attack ${on} ${lasts}, taking damage before HP`,
            ]
          : []),
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
    case 'status':
      return [
        `${cap(and(onFoes(stage, points)))} on foes`,
        ...moveText(stage),
        ...cost,
        ...reachText(stage),
      ].join(' · ');
    case 'move':
      return [...moveText(stage).map((t, i) => (i === 0 ? cap(t) : t)), ...cost].join(' · ');
    default: {
      const parts = [
        `${power}× ${stage.damage === 'magic' ? 'magic' : 'physical'} attack`,
        ...(stage.hits
          ? [`${stage.hits.count} hits, ${num(stage.hits.every_ms / 1000)}s apart`]
          : []),
        ...onFoes(stage, points),
        ...moveText(stage),
        ...cost,
        ...reachText(stage),
      ];
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
    const shield = stage.shield?.power.per_point
      ? [`shield +${num(stage.shield.power.per_point)}×`]
      : [];
    return [...bonuses, ...shield, ...inflictPerPoint(stage.on_hit)].join(' · ');
  }
  if (skill.effect === 'move') {
    return '';
  }
  if (skill.effect === 'status') {
    return inflictPerPoint(stage.status).join(' · ');
  }
  return skill.effect === 'heal'
    ? `heal +${num(stage.power.per_point)}× magic attack`
    : [
        ...(stage.power.per_point > 0 ? [`power +${num(stage.power.per_point)}×`] : []),
        ...inflictPerPoint(stage.status),
      ].join(' · ');
}
