// The chains panel under the skill window's grid (skill-traits.md section 7): each of the class's roots and what its key turns into, in order, so the order reads as the rule.
import { useState } from 'react';

import { SkillIcon } from '@/components/SkillIcon';
import { readStored, writeStored } from '@/lib/storage';
import { chainsOf, conditionText, shortCondition } from '@/progression/chains';
import { pointsIn, skillById, stageAt } from '@/progression/skills';

import { cardProps, dragSkill } from './cardTarget';
import type { CardTarget } from './cardTarget';
import type { SkillPointMap } from './useSkillPoints';

/** Whether the panel is folded, kept per browser; it starts open. */
const FOLD_KEY = 'finga.skills.chains';

/** A skill in the panel: its small icon, greyed with no points, and its stage name; it shows its card, and a root drags as its tile does. */
function ChainSkill({
  id,
  root,
  invested,
  cardId,
  target,
}: {
  readonly id: string;
  readonly root: boolean;
  readonly invested: SkillPointMap;
  readonly cardId: string;
  readonly target: CardTarget;
}) {
  const skill = skillById(id);
  if (!skill) {
    return null;
  }
  const points = pointsIn(skill, invested);
  const name = skill.stages[stageAt(skill, points)]!.name;
  const drags = root && skill.kind === 'active';
  return (
    <span
      className={`skill-chain-skill${drags ? ' skill-tile-drags' : ''}`}
      role="img"
      aria-label={name}
      {...cardProps(target, cardId)}
      draggable={drags}
      {...(drags && { onDragStart: dragSkill(id, target) })}
    >
      <SkillIcon skill={skill} points={points} size="window" off={points === 0} />
      <span className="skill-chain-name">{name}</span>
    </span>
  );
}

export function ChainsPanel({
  cls,
  invested,
  cardId,
  target,
}: {
  readonly cls: string;
  readonly invested: SkillPointMap;
  readonly cardId: string;
  /** The card's say for a skill shown from the element named from. */
  readonly target: (id: string, from: string) => CardTarget;
}) {
  const [open, setOpen] = useState(() => readStored(FOLD_KEY) !== 'closed');
  const chains = chainsOf(cls);
  if (chains.length === 0) {
    return null;
  }
  const fold = () => {
    writeStored(FOLD_KEY, open ? 'closed' : 'open');
    setOpen(!open);
  };
  const nameOf = (id: string) => {
    const s = skillById(id);
    return s?.stages[stageAt(s, pointsIn(s, invested))]?.name ?? id;
  };
  return (
    <section className="skill-chains" aria-label="Chains">
      <h3 className="skill-class">
        <button type="button" className="skill-chains-fold" aria-expanded={open} onClick={fold}>
          Chains: what each key turns into
          <span aria-hidden="true">{open ? ' ▾' : ' ▸'}</span>
        </button>
      </h3>
      {open && (
        <ul className="skill-chain-map">
          {chains.map((c) => {
            const root = nameOf(c.root);
            return (
              <li key={c.root} className="skill-chain-row" aria-label={`${root}’s chain`}>
                <ChainSkill
                  id={c.root}
                  root
                  invested={invested}
                  cardId={cardId}
                  target={target(c.root, `chain:${c.root}`)}
                />
                <ul className="skill-chain-steps">
                  {c.follow.map((f, i) => (
                    <li key={f.skill} className="skill-chain-step">
                      <span className="skill-chain-link" aria-hidden="true">
                        {i === 0 ? '─' : '└'}
                      </span>
                      <span className="skill-chain-when" title={conditionText(f.when, root)}>
                        {shortCondition(f.when)}
                      </span>
                      <span className="skill-chain-arrow" aria-hidden="true">
                        ▶
                      </span>
                      <ChainSkill
                        id={f.skill}
                        root={false}
                        invested={invested}
                        cardId={cardId}
                        target={target(f.skill, `chain:${c.root}:${f.skill}`)}
                      />
                    </li>
                  ))}
                </ul>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
