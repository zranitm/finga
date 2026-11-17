// Stages and evolutions are worked out here from game data and the points; the server's sheet holds only the points.
// Each chain shows under its root: the follow-ups its key turns into, and while what (skill-chains.md section 5).
import { Button } from '@/components/Button';
import { dragTile, SkillIcon } from '@/components/SkillIcon';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { chainsOf, conditionText, keyable } from '@/progression/chains';
import { className } from '@/progression/classes';
import { learnable, pointsIn, skillById, stageAt } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';

import { actionText, effectText, perPointText } from './skillText';
import type { CooldownClock } from './skillText';
import { sumOf, useSkillPoints } from './useSkillPoints';
import type { SkillPoints } from './useSkillPoints';

export interface SkillWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

/** A skill's name at the stage its points reach. */
function nameAt(id: string, invested: Readonly<Record<string, number>>): string {
  const s = skillById(id);
  return s?.stages[stageAt(s, invested[id] ?? 0)]?.name ?? id;
}

/** Where a skill comes up on another's key for cls, each root's key and while what: "on Strike’s key while the target is over 3 m away". */
function comesOn(cls: string, skill: string, invested: Readonly<Record<string, number>>): string[] {
  return chainsOf(cls).flatMap((c) =>
    c.follow
      .filter((f) => f.skill === skill)
      .map((f) => {
        const root = nameAt(c.root, invested);
        return `on ${root}’s key ${conditionText(f.when, root)}`;
      }),
  );
}

/** The follow-ups a root's key turns into for cls, in their order, under the root's row. */
function ChainList({
  cls,
  root,
  invested,
}: {
  readonly cls: string;
  readonly root: string;
  readonly invested: Readonly<Record<string, number>>;
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

function SkillRow({
  cls,
  skill,
  points,
  sp,
}: {
  readonly cls: string;
  readonly skill: Skill;
  readonly points: number;
  readonly sp: SkillPoints;
}) {
  const staged = sp.staged[skill.id] ?? 0;
  const total = points + staged;
  const now = skill.stages[stageAt(skill, points)]!;
  const then = skill.stages[stageAt(skill, total)]!;
  const next = skill.stages[stageAt(skill, total) + 1];
  const active = skill.kind === 'active';
  // A granted skill is known with its class and takes no points; another takes none below its level.
  const locked = character.use((s) => !skill.granted && (s.character?.level ?? 1) < skill.level);
  // The cooldown the skill bar runs: cut by the character's cooldown speed, in the world's ticks.
  const speed = character.use((s) => s.character?.sheet?.derived?.cooldownSpeed ?? 0);
  const tickMs = skillBar.use((s) => s.tickMs);
  const cooldown = skillBar.use((s) => s.cooldowns[skill.id]);
  const clock: CooldownClock = { speed, tickMs };
  return (
    <li
      className={`skill${active ? ' skill-active' : ''}`}
      draggable={active}
      title={active ? 'Drag onto the skill bar to use it with keys 1 to 6.' : undefined}
      onDragStart={(e) => {
        e.dataTransfer.setData(SKILL_DRAG_TYPE, skill.id);
        e.dataTransfer.effectAllowed = 'copy';
        dragTile(e);
      }}
    >
      <div className="skill-head">
        <SkillIcon
          skill={skill}
          points={points}
          size="window"
          off={points === 0}
          cooldown={cooldown}
        />
        <strong>{now.name}</strong>
        {then !== now && <span className="stat-preview"> → {then.name}</span>}
        <span className="skill-points">
          {skill.granted ? 'Granted' : `${points} ${points === 1 ? 'point' : 'points'}`}
          {staged > 0 && <span className="stat-preview"> +{staged}</span>}
        </span>
        {locked ? (
          <span className="skill-locked" title={`Takes its first point at level ${skill.level}`}>
            Lv {skill.level}
          </span>
        ) : (
          sp.unspent > 0 &&
          !skill.granted && (
            <button
              type="button"
              className="stat-plus"
              aria-label={`Add a point to ${now.name}`}
              disabled={sp.left === 0 || sp.busy}
              onClick={() => sp.stage(skill.id)}
            >
              +
            </button>
          )
        )}
      </div>
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
      {active && <ChainedOn cls={cls} skill={skill.id} invested={sp.invested} />}
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
      <ChainList cls={cls} root={skill.id} invested={sp.invested} />
    </li>
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
  readonly invested: Readonly<Record<string, number>>;
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

export function SkillWindow({ socket, onClose }: SkillWindowProps) {
  const cls = character.use((s) => s.character?.class ?? '');
  const sp = useSkillPoints(socket);
  const skills = learnable(cls);
  const classes = [...new Set(skills.map((s) => s.class))];
  const staged = sumOf(sp.staged) > 0;
  return (
    <WorldWindow
      name="skills"
      title="Skills"
      onClose={onClose}
      initialX={940}
      initialY={120}
      footer={
        sp.notice ? (
          <p className="error" role="alert">
            {sp.notice}
          </p>
        ) : null
      }
    >
      <div className="skill-window">
        <div className="character-points" role="group" aria-label="Skill points">
          <span className="character-points-left">
            {sp.unspent > 0 ? `Skill points: ${sp.left}` : 'No skill points to spend'}
          </span>
          {staged && (
            <>
              <Button variant="quiet" disabled={sp.busy} onClick={sp.undo}>
                Undo
              </Button>
              <Button disabled={sp.busy} onClick={sp.apply}>
                Apply
              </Button>
            </>
          )}
        </div>
        {classes.map((c) => (
          <section key={c} aria-label={`${className(c)} skills`}>
            <h3 className="skill-class">{className(c)}</h3>
            <ul className="skills">
              {skills
                .filter((s) => s.class === c)
                .map((s) => (
                  <SkillRow
                    key={s.id}
                    cls={cls}
                    skill={s}
                    points={pointsIn(s, sp.invested)}
                    sp={sp}
                  />
                ))}
            </ul>
          </section>
        ))}
      </div>
    </WorldWindow>
  );
}
