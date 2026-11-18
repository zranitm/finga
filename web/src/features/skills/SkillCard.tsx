// A skill's card beside the pointer (skill-traits.md section 7): everything about it, its stage worked out from game data and the points, its chains included.
import { SkillIcon } from '@/components/SkillIcon';
import { chainsOf, conditionText, keyable } from '@/progression/chains';
import { className } from '@/progression/classes';
import { skillById, specAt, stageAt, traitOpen } from '@/progression/skills';
import type { Skill } from '@/progression/skills';

import { actionText, effectText, perPointText } from './skillText';
import type { CooldownClock } from './skillText';
import type { SkillPointMap } from './useSkillPoints';

/** A skill's name at the stage its points reach. */
function nameAt(id: string, invested: SkillPointMap): string {
  const s = skillById(id);
  return s?.stages[stageAt(s, invested[id] ?? 0)]?.name ?? id;
}

/** Where a skill comes up on another's key for cls, each root's key and while what: "on Strike’s key while the target is over 3 m away". */
function comesOn(cls: string, skill: string, invested: SkillPointMap): string[] {
  return chainsOf(cls).flatMap((c) =>
    c.follow
      .filter((f) => f.skill === skill)
      .map((f) => {
        const root = nameAt(c.root, invested);
        return `on ${root}’s key ${conditionText(f.when, root)}`;
      }),
  );
}

/** The follow-ups a root's key turns into for cls, in their order. */
function ChainList({
  cls,
  root,
  invested,
}: {
  readonly cls: string;
  readonly root: string;
  readonly invested: SkillPointMap;
}) {
  const chain = chainsOf(cls).find((c) => c.root === root);
  if (!chain) {
    return null;
  }
  const name = nameAt(root, invested);
  return (
    <ul className="skill-chain" aria-label={`${name}’s chain`}>
      {chain.follow.map((f) => (
        <li key={f.skill}>
          <strong>{nameAt(f.skill, invested)}</strong> {conditionText(f.when, name)}
        </li>
      ))}
    </ul>
  );
}

/** Where a follow-up comes up: on its roots' keys, and for one that can't stand on a key of its own, that it can't. */
function ChainedOn({
  cls,
  skill,
  invested,
}: {
  readonly cls: string;
  readonly skill: string;
  readonly invested: SkillPointMap;
}) {
  const on = comesOn(cls, skill, invested);
  if (on.length === 0) {
    return null;
  }
  const where = on.join('; ');
  return (
    <p className="skill-text muted">
      {keyable(cls, skill)
        ? `Also ${where}.`
        : `Comes up ${where}; it can’t go on a key of its own.`}
    </p>
  );
}

export interface SkillCardProps {
  readonly id: string;
  /** The character's class, whose chains it reads. */
  readonly cls: string;
  readonly skill: Skill;
  readonly points: number;
  readonly staged: number;
  readonly invested: SkillPointMap;
  /** The trait id chosen for it, the default's for a skill on its default. */
  readonly trait: string;
  readonly locked: boolean;
  readonly clock: CooldownClock;
}

/** Both traits, each a line, the chosen one marked, and the extra's evolution while it's shut. */
function Traits({
  skill,
  points,
  trait,
}: {
  readonly skill: Skill;
  readonly points: number;
  readonly trait: string;
}) {
  const last = skill.stages.at(-1)!;
  return (
    <ul className="skill-trait-list" aria-label="Traits">
      {skill.traits.map((t, k) => (
        <li key={t.id} className={t.id === trait ? 'skill-trait-chosen' : undefined}>
          <span aria-hidden="true">{t.id === trait ? '◆' : '◇'} </span>
          <strong>{t.name}</strong>
          {t.id === trait && ' (chosen)'}: {t.description}
          {k === 1 && !traitOpen(skill, points) && last.at > 0 && (
            <span className="muted"> Opens at {last.at} points.</span>
          )}
        </li>
      ))}
    </ul>
  );
}

export function SkillCard({
  id,
  cls,
  skill,
  points,
  staged,
  invested,
  trait,
  locked,
  clock,
}: SkillCardProps) {
  const total = points + staged;
  const now = specAt(skill, points, trait);
  const then = specAt(skill, total, trait);
  const next = skill.stages[stageAt(skill, total) + 1];
  const active = skill.kind === 'active';
  return (
    <div id={id} className="item-card skill-card" role="tooltip">
      <div className="skill-card-head">
        <SkillIcon skill={skill} points={points} size="window" off={points === 0} />
        <div>
          <div className="item-card-name">
            {now.name}
            {then !== now && <span className="stat-preview"> → {then.name}</span>}
          </div>
          <div className="item-card-type">
            {className(skill.class)} · {active ? 'Active' : 'Passive'} ·{' '}
            {skill.granted ? 'Granted' : `${points} ${points === 1 ? 'point' : 'points'}`}
            {staged > 0 && <span className="skill-staged"> +{staged}</span>}
          </div>
        </div>
      </div>
      {locked && (
        <p className="skill-text skill-locked">
          Lv {skill.level}: takes its first point at level {skill.level}
        </p>
      )}
      <p className="skill-text">{then.description}</p>
      {points > 0 ? (
        <p className="skill-text">
          {effectText(skill, now, points, clock)}
          {staged > 0 && (
            <span className="stat-preview"> → {effectText(skill, then, total, clock)}</span>
          )}
        </p>
      ) : (
        <>
          <p className="skill-text muted">
            {active ? 'Usable' : 'Works'} once it has a point
            {staged === 0 && `: ${effectText(skill, now, 1, clock)}`}
          </p>
          {staged > 0 && (
            <p className="skill-text stat-preview">
              With {total} {total === 1 ? 'point' : 'points'}:{' '}
              {effectText(skill, then, total, clock)}
            </p>
          )}
        </>
      )}
      {active && <p className="skill-text muted">{actionText(then)}</p>}
      {active && <ChainedOn cls={cls} skill={skill.id} invested={invested} />}
      {!skill.granted && (
        <>
          <p className="skill-text muted">Each point: {perPointText(skill, then)}</p>
          <p className="skill-text muted">
            {next
              ? `Evolves into ${next.name} at ${next.at} points (${next.at - total} more).`
              : 'Fully evolved.'}
          </p>
        </>
      )}
      {skill.traits.length > 0 && <Traits skill={skill} points={points} trait={trait} />}
      <ChainList cls={cls} root={skill.id} invested={invested} />
      {active && keyable(cls, skill.id) && (
        <p className="skill-text muted">Drag onto the skill bar to use it with its keys.</p>
      )}
    </div>
  );
}
