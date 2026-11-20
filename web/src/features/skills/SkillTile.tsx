// A skill's tile in the skill window's grids: its icon, points and name, a + for a point while there are some to spend, and under an active skill its two traits to choose between.
import { Fragment } from 'react';
import type { CSSProperties, FocusEvent, MouseEvent } from 'react';

import { SkillIcon } from '@/components/SkillIcon';
import { keyable } from '@/progression/chains';
import { colorOf, stageAt, traitOpen } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { skillBar } from '@/state/skill-bar';

import { cardProps, dragSkill } from './cardTarget';
import type { CardTarget } from './cardTarget';
import type { SkillPoints } from './useSkillPoints';
import type { TraitChoice } from './useTraits';

/** A follow-up's chain mark: two links. */
function ChainMark() {
  return (
    <svg className="skill-tile-chain" viewBox="0 0 12 12" aria-hidden="true">
      <rect x="1" y="4" width="6" height="4" rx="2" />
      <rect x="5" y="4" width="6" height="4" rx="2" />
    </svg>
  );
}

/** A trait's diamond: filled in the skill's colour when chosen, an outline with a lock while the extra is shut. */
function Diamond({ chosen, shut }: { readonly chosen: boolean; readonly shut: boolean }) {
  return (
    <svg
      className={`skill-trait-mark${shut ? ' skill-trait-shut' : ''}`}
      viewBox="0 0 12 12"
      aria-hidden="true"
    >
      <polygon points="6,1 11,6 6,11 1,6" className={chosen ? 'skill-trait-fill' : undefined} />
      {shut && <rect x="4.25" y="5" width="3.5" height="3" rx="0.5" className="skill-trait-lock" />}
    </svg>
  );
}

/** The card props of a diamond inside a tile: its own hover and focus, kept from the tile's, which would show the skill's card over it. */
function ownCard(t: CardTarget, cardId: string) {
  const p = cardProps(t, cardId);
  return {
    ...p,
    onMouseMove: (e: MouseEvent) => {
      e.stopPropagation();
      p.onMouseMove(e);
    },
    onFocus: (e: FocusEvent<HTMLElement>) => {
      e.stopPropagation();
      p.onFocus(e);
    },
    onBlur: (e: FocusEvent<HTMLElement>) => {
      e.stopPropagation();
      p.onBlur();
    },
  };
}

/** An active skill's two traits under its tile: a click on the other sends the choice at once, unless the extra is shut, the player is in combat or a choice is on its way; one is on its way at a time. */
function TraitButtons({
  skill,
  points,
  name,
  choice,
  inCombat,
  cardId,
  target,
}: {
  readonly skill: Skill;
  readonly points: number;
  readonly name: string;
  readonly choice: TraitChoice;
  readonly inCombat: boolean;
  readonly cardId: string;
  readonly target: (trait: number) => CardTarget;
}) {
  const chosen = choice.chosen(skill);
  const style = {
    '--skill': `#${colorOf(skill, skill.stages.at(-1)!).toString(16).padStart(6, '0')}`,
  } as CSSProperties;
  return (
    <span
      className={`skill-traits${inCombat ? ' skill-traits-fighting' : ''}`}
      role="group"
      aria-label={`${name}’s traits`}
      style={style}
    >
      {skill.traits.map((t, k) => {
        const shut = k === 1 && !traitOpen(skill, points);
        const pending = choice.pending?.skill === skill.id && choice.pending.trait === t.id;
        const blocked = shut || inCombat || choice.pending !== null;
        return (
          <button
            key={t.id}
            type="button"
            className={`skill-trait${pending ? ' skill-trait-pending' : ''}`}
            aria-label={`${t.name}${t.id === chosen ? ', chosen' : shut ? ', not open yet' : ''}`}
            aria-pressed={t.id === chosen}
            aria-disabled={t.id !== chosen && blocked}
            {...ownCard(target(k), cardId)}
            onClick={() => {
              if (!blocked) {
                choice.choose(skill, t.id);
              }
            }}
          >
            <Diamond chosen={t.id === chosen} shut={shut} />
          </button>
        );
      })}
    </span>
  );
}

export function SkillTile({
  cls,
  skill,
  points,
  locked,
  sp,
  choice,
  inCombat,
  cardId,
  target,
  traitTarget,
}: {
  readonly cls: string;
  readonly skill: Skill;
  readonly points: number;
  /** Below the level it takes its first point at. */
  readonly locked: boolean;
  /** The points to spend, null where there are none, as in the Arena tab. */
  readonly sp: SkillPoints | null;
  readonly choice: TraitChoice;
  readonly inCombat: boolean;
  readonly cardId: string;
  readonly target: CardTarget;
  readonly traitTarget: (trait: number) => CardTarget;
}) {
  const staged = sp?.staged[skill.id] ?? 0;
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
        {sp && !locked && sp.unspent > 0 && !skill.granted && (
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
      {active && skill.traits.length === 2 && (
        <TraitButtons
          skill={skill}
          points={points}
          name={now.name}
          choice={choice}
          inCombat={inCombat}
          cardId={cardId}
          target={traitTarget}
        />
      )}
    </li>
  );
}

/** A granted skill is known with its class and takes no points; another takes none below its level. */
export function lockedAt(skill: Skill, level: number): boolean {
  return !skill.granted && level < skill.level;
}

/** A class's skills in the order their levels open them, then the class's own order. */
export function byLevel(skills: readonly Skill[]): Skill[] {
  return [...skills].sort((a, b) => a.level - b.level);
}
