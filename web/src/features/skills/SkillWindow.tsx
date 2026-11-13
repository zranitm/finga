// Stages and evolutions are worked out here from game data and the points; the server's sheet holds only the points.
import { Button } from '@/components/Button';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { className } from '@/progression/classes';
import { learnable, stageAt } from '@/progression/skills';
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

function SkillRow({
  skill,
  points,
  sp,
}: {
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
  // The cooldown the skill bar runs: cut by the character's cooldown speed, in the world's ticks.
  const speed = character.use((s) => s.character?.sheet?.derived?.cooldownSpeed ?? 0);
  const tickMs = skillBar.use((s) => s.tickMs);
  const clock: CooldownClock = { speed, tickMs };
  return (
    <li
      className={`skill${active ? ' skill-active' : ''}`}
      draggable={active}
      title={active ? 'Drag onto the skill bar to use it with keys 1 to 6.' : undefined}
      onDragStart={(e) => {
        e.dataTransfer.setData(SKILL_DRAG_TYPE, skill.id);
        e.dataTransfer.effectAllowed = 'copy';
      }}
    >
      <div className="skill-head">
        <strong>{now.name}</strong>
        {then !== now && <span className="stat-preview"> → {then.name}</span>}
        <span className="skill-points">
          {points} {points === 1 ? 'point' : 'points'}
          {staged > 0 && <span className="stat-preview"> +{staged}</span>}
        </span>
        {sp.unspent > 0 && (
          <button
            type="button"
            className="stat-plus"
            aria-label={`Add a point to ${now.name}`}
            disabled={sp.left === 0 || sp.busy}
            onClick={() => sp.stage(skill.id)}
          >
            +
          </button>
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
      <p className="skill-text muted">Each point: {perPointText(skill, then)}</p>
      <p className="skill-text muted">
        {next
          ? `Evolves into ${next.name} at ${next.at} points (${next.at - total} more).`
          : 'Fully evolved.'}
      </p>
    </li>
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
                  <SkillRow key={s.id} skill={s} points={sp.invested[s.id] ?? 0} sp={sp} />
                ))}
            </ul>
          </section>
        ))}
      </div>
    </WorldWindow>
  );
}
