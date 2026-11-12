// What the skill window says a skill does, by its effect, from the numbers the server uses.
import { itemStats, statText } from '@/components/item-stats';
import { mpAt, scaledAt } from '@/progression/skills';
import type { Scaled, Skill, Stage } from '@/progression/skills';

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

/** Who a heal reaches, as the world picks them: the caster if hurt, then the nearest hurt party members in its shape, up to its targets. */
function healReach(stage: Stage): string {
  const within = stage.shape?.radius ? ` within ${stage.shape.radius}` : '';
  return stage.targets > 1
    ? `you and the nearest hurt party members${within}, up to ${stage.targets} in all`
    : `you if hurt, else the nearest hurt party member${within}`;
}

/** What the stage does with points invested. */
export function effectText(skill: Skill, stage: Stage, points: number): string {
  if (skill.kind === 'passive') {
    return bonusesAt(stage.bonus, points).join(' · ');
  }
  const cost = [`${mpAt(stage, points)} MP`, `${num(stage.cooldown_ms / 1000)}s cooldown`];
  const power = num(scaledAt(stage.power, points));
  switch (skill.effect) {
    case 'buff':
      // A buff is on the caster alone, so its shape reaches no one else.
      return [
        `${and(bonusesAt(stage.bonus, points))} on you for ${num(stage.duration_ms / 1000)}s`,
        ...cost,
      ].join(' · ');
    case 'heal':
      return [`Heals ${healReach(stage)}, by ${power}× magic attack`, ...cost].join(' · ');
    default: {
      const parts = [
        `${power}× ${stage.damage === 'magic' ? 'magic' : 'physical'} attack`,
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

/** What each point adds in the stage. */
export function perPointText(skill: Skill, stage: Stage): string {
  const bonuses = Object.entries(stage.bonus).map(([stat, s]) => bonusText(stat, s.per_point));
  const mp = `MP +${num(stage.mp.per_point)}`;
  if (skill.kind === 'passive') {
    return bonuses.join(' · ');
  }
  switch (skill.effect) {
    case 'buff':
      return [...bonuses, mp].join(' · ');
    case 'heal':
      return [`heal +${num(stage.power.per_point)}× magic attack`, mp].join(' · ');
    default:
      return [`power +${num(stage.power.per_point)}×`, mp].join(' · ');
  }
}
