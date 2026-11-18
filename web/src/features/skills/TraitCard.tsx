// A trait's card beside the pointer (skill-traits.md section 7): what the skill does with it, both traits at the last stage so their numbers compare, and whether a click chooses it.
import type { Skill } from '@/progression/skills';
import { stageAt, traitOpen } from '@/progression/skills';

import { actionText, effectText } from './skillText';
import type { CooldownClock } from './skillText';

export interface TraitCardProps {
  readonly id: string;
  readonly skill: Skill;
  /** Which of the skill's traits: 0 the default, 1 the extra. */
  readonly trait: number;
  readonly points: number;
  readonly chosen: boolean;
  readonly inCombat: boolean;
  readonly clock: CooldownClock;
}

/** What a click on a trait's diamond does, or why it doesn't. */
function choiceText(
  skill: Skill,
  trait: number,
  points: number,
  chosen: boolean,
  inCombat: boolean,
): string {
  const last = skill.stages.at(-1)!;
  if (chosen) {
    return 'Chosen';
  }
  if (trait === 1 && !traitOpen(skill, points)) {
    return last.at > 0
      ? `Opens at ${last.at} points, as ${last.name}`
      : 'Opens with its first point';
  }
  return inCombat ? 'In combat: traits change once out of it.' : 'Click to choose';
}

export function TraitCard({ id, skill, trait, points, chosen, inCombat, clock }: TraitCardProps) {
  const t = skill.traits[trait]!;
  const last = skill.stages.at(-1)!;
  const at = Math.max(points, last.at, 1); // the last stage, so the default's numbers and the extra's compare
  const stage = t.stage ?? skill.stages[stageAt(skill, at)]!;
  return (
    <div id={id} className="item-card skill-card" role="tooltip">
      <div className="item-card-name">{t.name}</div>
      <div className="item-card-type">
        {trait === 0 ? 'Default' : 'Extra'} · {stage.name}
      </div>
      <p className="skill-text">{t.description}</p>
      <p className="skill-text">
        {at > points && `With ${at} ${at === 1 ? 'point' : 'points'}: `}
        {effectText(skill, stage, at, clock)}
      </p>
      {skill.kind === 'active' && <p className="skill-text muted">{actionText(stage)}</p>}
      <p className={`skill-text${chosen ? '' : ' muted'}`}>
        {choiceText(skill, trait, points, chosen, inCombat)}
      </p>
    </div>
  );
}
