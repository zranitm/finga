// The skill window (skill-traits.md section 7): a grid of tiles per class, each skill's details in a card beside the pointer, on hover or focus, and under an active one its two traits to choose between.
import { Fragment, useId, useState } from 'react';
import type { CSSProperties, FocusEvent, MouseEvent } from 'react';

import { Button } from '@/components/Button';
import { SkillIcon } from '@/components/SkillIcon';
import { BesidePointer } from '@/features/shared/BesidePointer';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { Point } from '@/lib/placement';
import type { GameSocket } from '@/net/socket';
import { keyable } from '@/progression/chains';
import { className } from '@/progression/classes';
import { colorOf, learnable, pointsIn, skillById, stageAt, traitOpen } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { character } from '@/state/character';
import { combat } from '@/state/combat';
import { skillBar } from '@/state/skill-bar';

import { cardProps, dragSkill } from './cardTarget';
import type { CardTarget } from './cardTarget';
import { ChainsPanel } from './ChainsPanel';
import { SkillCard } from './SkillCard';
import type { CooldownClock } from './skillText';
import { TraitCard } from './TraitCard';
import { sumOf, useSkillPoints } from './useSkillPoints';
import type { SkillPoints } from './useSkillPoints';
import { useTraits } from './useTraits';
import type { TraitChoice } from './useTraits';

export interface SkillWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

/** The skill whose card shows, the element showing it, a tile, a panel icon or a trait's diamond, and where; a diamond shows its trait's card. */
interface Shown {
  readonly id: string;
  readonly from: string;
  readonly at: Point;
  readonly trait?: number;
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

function SkillTile({
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
  readonly sp: SkillPoints;
  readonly choice: TraitChoice;
  readonly inCombat: boolean;
  readonly cardId: string;
  readonly target: CardTarget;
  readonly traitTarget: (trait: number) => CardTarget;
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
  const choice = useTraits(socket);
  const inCombat = combat.use((s) => s.inCombat);
  const [shown, setShown] = useState<Shown | null>(null);
  const cardId = useId();
  // Each element showing the card is named by from, so a skill shown from the panel describes only the icon under the pointer.
  const target = (id: string, from: string, trait?: number): CardTarget => ({
    shown: shown?.from === from,
    onShow: (at) => setShown({ id, from, at, ...(trait !== undefined && { trait }) }),
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
        sp.notice || choice.notice ? (
          <>
            {[sp.notice, choice.notice].map(
              (n, i) =>
                n && (
                  <p key={i} className="error" role="alert">
                    {n}
                  </p>
                ),
            )}
          </>
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
        {inCombat && (
          <p className="skill-text muted skill-combat">In combat: traits change once out of it.</p>
        )}
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
                  choice={choice}
                  inCombat={inCombat}
                  cardId={cardId}
                  target={target(s.id, `tile:${s.id}`)}
                  traitTarget={(k) => target(s.id, `trait:${s.id}:${k}`, k)}
                />
              ))}
            </ul>
          </section>
        ))}
        <ChainsPanel cls={cls} invested={sp.invested} cardId={cardId} target={target} />
      </div>
      {shown && card && (
        <BesidePointer at={shown.at}>
          {shown.trait === undefined ? (
            <SkillCard
              id={cardId}
              cls={cls}
              skill={card}
              points={pointsIn(card, sp.invested)}
              staged={sp.staged[card.id] ?? 0}
              invested={sp.invested}
              trait={choice.chosen(card)}
              locked={lockedAt(card, level)}
              clock={clock}
            />
          ) : (
            <TraitCard
              id={cardId}
              skill={card}
              trait={shown.trait}
              points={pointsIn(card, sp.invested)}
              chosen={card.traits[shown.trait]?.id === choice.chosen(card)}
              inCombat={inCombat}
              clock={clock}
            />
          )}
        </BesidePointer>
      )}
    </WorldWindow>
  );
}
