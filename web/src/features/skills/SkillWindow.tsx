// The skill window (skill-traits.md section 7): a grid of tiles per class, each skill's details in a card beside the pointer, on hover or focus.
import { Fragment, useId, useState } from 'react';

import { Button } from '@/components/Button';
import { SkillIcon } from '@/components/SkillIcon';
import { BesidePointer } from '@/features/shared/BesidePointer';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { Point } from '@/lib/placement';
import type { GameSocket } from '@/net/socket';
import { keyable } from '@/progression/chains';
import { className } from '@/progression/classes';
import { learnable, pointsIn, skillById, stageAt } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { character } from '@/state/character';
import { skillBar } from '@/state/skill-bar';

import { cardProps, dragSkill } from './cardTarget';
import type { CardTarget } from './cardTarget';
import { ChainsPanel } from './ChainsPanel';
import { SkillCard } from './SkillCard';
import type { CooldownClock } from './skillText';
import { sumOf, useSkillPoints } from './useSkillPoints';
import type { SkillPoints } from './useSkillPoints';

export interface SkillWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

/** The skill whose card shows, the element showing it, a tile or a panel icon, and where. */
interface Shown {
  readonly id: string;
  readonly from: string;
  readonly at: Point;
}

/** A follow-up's chain mark: two links. */
function ChainMark() {
  return (
    <svg className="skill-tile-chain" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1" y="4" width="6" height="4" rx="2" />
      <rect x="5" y="4" width="6" height="4" rx="2" />
    </svg>
  );
}

function SkillTile({
  cls,
  skill,
  points,
  locked,
  sp,
  cardId,
  target,
}: {
  readonly cls: string;
  readonly skill: Skill;
  readonly points: number;
  /** Below the level it takes its first point at. */
  readonly locked: boolean;
  readonly sp: SkillPoints;
  readonly cardId: string;
  readonly target: CardTarget;
}) {
  const staged = sp.staged[skill.id] ?? 0;
  const now = skill.stages[stageAt(skill, points)]!;
  const active = skill.kind === 'active';
  const onKey = keyable(cls, skill.id);
  const cooldown = skillBar.use((s) => s.cooldowns[skill.id]);
  const drags = active && onKey;
  return (
    <li
      className={`skill-tile${drags ? ' skill-tile-drags' : ''}`}
      aria-label={now.name}
      {...cardProps(target, cardId)}
      draggable={drags}
      {...(drags && { onDragStart: dragSkill(skill.id, target) })}
    >
      <span className="skill-tile-icon">
        <SkillIcon
          skill={skill}
          points={points}
          size="grid"
          off={points === 0}
          cooldown={cooldown}
        />
        {active && !onKey && <ChainMark />}
        {locked && <span className="skill-tile-lock">Lv {skill.level}</span>}
      </span>
      <span className="skill-tile-foot">
        <span className="skill-tile-points">
          {skill.granted ? '' : points}
          {staged > 0 && <span className="skill-staged">+{staged}</span>}
        </span>
        {!locked && sp.unspent > 0 && !skill.granted && (
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
      </span>
      <span className="skill-tile-name" title={now.name}>
        {now.name.split(' ').map((word, i) => (
          <Fragment key={i}>
            {i > 0 && ' '}
            <span className="skill-tile-word">{word}</span>
          </Fragment>
        ))}
      </span>
    </li>
  );
}

/** A granted skill is known with its class and takes no points; another takes none below its level. */
function lockedAt(skill: Skill, level: number): boolean {
  return !skill.granted && level < skill.level;
}

/** A class's skills in the order their levels open them, then the class's own order. */
function byLevel(skills: readonly Skill[]): Skill[] {
  return [...skills].sort((a, b) => a.level - b.level);
}

export function SkillWindow({ socket, onClose }: SkillWindowProps) {
  const cls = character.use((s) => s.character?.class ?? '');
  const level = character.use((s) => s.character?.level ?? 1);
  // The cooldown the skill bar runs: cut by the character's cooldown speed, in the world's ticks.
  const speed = character.use((s) => s.character?.sheet?.derived?.cooldownSpeed ?? 0);
  const tickMs = skillBar.use((s) => s.tickMs);
  const clock: CooldownClock = { speed, tickMs };
  const sp = useSkillPoints(socket);
  const [shown, setShown] = useState<Shown | null>(null);
  const cardId = useId();
  // Each element showing the card is named by from, so a skill shown from the panel describes only the icon under the pointer.
  const target = (id: string, from: string): CardTarget => ({
    shown: shown?.from === from,
    onShow: (at) => setShown({ id, from, at }),
    onHide: () => setShown((was) => (was?.from === from ? null : was)),
  });
  const skills = learnable(cls);
  const classes = [...new Set(skills.map((s) => s.class))].reverse(); // the Beginner first, then each class it led to
  const staged = sumOf(sp.staged) > 0;
  const card = shown && skillById(shown.id);
  return (
    <WorldWindow
      name="skills"
      title="Skills"
      onClose={onClose}
      initialX={900}
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
            <ul className="skill-grid">
              {byLevel(skills.filter((s) => s.class === c)).map((s) => (
                <SkillTile
                  key={s.id}
                  cls={cls}
                  skill={s}
                  points={pointsIn(s, sp.invested)}
                  locked={lockedAt(s, level)}
                  sp={sp}
                  cardId={cardId}
                  target={target(s.id, `tile:${s.id}`)}
                />
              ))}
            </ul>
          </section>
        ))}
        <ChainsPanel cls={cls} invested={sp.invested} cardId={cardId} target={target} />
      </div>
      {shown && card && (
        <BesidePointer at={shown.at}>
          <SkillCard
            id={cardId}
            cls={cls}
            skill={card}
            points={pointsIn(card, sp.invested)}
            staged={sp.staged[card.id] ?? 0}
            invested={sp.invested}
            locked={lockedAt(card, level)}
            clock={clock}
          />
        </BesidePointer>
      )}
    </WorldWindow>
  );
}
