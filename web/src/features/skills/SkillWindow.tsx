// The skill window (skill-traits.md section 7): a grid of tiles per class, each skill's details in a card beside the pointer, on hover or focus, and under an active one its two traits to choose between. A promoted character's has an Arena tab too (pvp.md section 13): the build PvP matches fight with, every skill full and every trait open, and its arena bar.
import { useEffect, useId, useState } from 'react';

import { Button } from '@/components/Button';
import { Tabs } from '@/components/Tabs';
import { BesidePointer } from '@/features/shared/BesidePointer';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { pvp as pvpData } from '@/gamedata/pvp';
import type { Point } from '@/lib/placement';
import type { GameSocket } from '@/net/socket';
import { className, lineOf } from '@/progression/classes';
import { fullPoints, learnable, pointsIn, skillById } from '@/progression/skills';
import type { Skill } from '@/progression/skills';
import { arenaBuild } from '@/state/arena-build';
import { character } from '@/state/character';
import { combat } from '@/state/combat';
import { skillBar } from '@/state/skill-bar';

import { ArenaBar } from './ArenaBar';
import type { CardTarget } from './cardTarget';
import { ChainsPanel } from './ChainsPanel';
import { SkillCard } from './SkillCard';
import type { CooldownClock } from './skillText';
import { byLevel, lockedAt, SkillTile } from './SkillTile';
import { TraitCard } from './TraitCard';
import { useArenaTraits } from './useArenaTraits';
import { sumOf, useSkillPoints } from './useSkillPoints';
import { useTraits } from './useTraits';

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

type SkillTab = 'world' | 'arena';

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
  const promoted = lineOf(cls).length > 1;
  const [tab, setTab] = useState<SkillTab>('world');
  // Closing saves both bars' changes at once, as settings do, so a queue straight after fights with them.
  useEffect(
    () => () => {
      skillBar.getState().saveNow();
      arenaBuild.getState().saveNow();
    },
    [],
  );
  const arena = promoted && tab === 'arena';
  const arenaChoice = useArenaTraits();
  const bar = arenaBuild.use((s) => s.bar);
  // What the cards read: the character's own points and traits, or the arena build's, every skill full.
  const view = arena
    ? { invested: fullPoints(cls), choice: arenaChoice, inCombat: false }
    : { invested: sp.invested, choice, inCombat };
  const lockedNow = (s: Skill) => !arena && lockedAt(s, level);
  return (
    <WorldWindow
      name="skills"
      title="Skills"
      onClose={onClose}
      initialX={900}
      initialY={120}
      footer={
        !arena && (sp.notice || choice.notice) ? (
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
        {promoted && (
          <Tabs
            label="Skills"
            tabs={[
              { id: 'world', name: 'Skills' },
              { id: 'arena', name: 'Arena' },
            ]}
            chosen={tab}
            onChoose={(t) => {
              setShown(null);
              setTab(t);
            }}
          />
        )}
        {arena ? (
          bar === null ? (
            <p className="muted">Reading your arena build…</p>
          ) : (
            <>
              <p className="skill-text muted">
                PvP matches fight at level {pvpData.kit_level}, every skill full and every trait
                open. A match fights with this build from its next round.
              </p>
              {classes.map((c) => (
                <section key={c} aria-label={`${className(c)} skills`}>
                  <h3 className="skill-class">{className(c)}</h3>
                  <ul className="skill-grid">
                    {byLevel(skills.filter((s) => s.class === c && s.kind === 'active')).map(
                      (s) => (
                        <SkillTile
                          key={s.id}
                          cls={cls}
                          skill={s}
                          points={pointsIn(s, view.invested)}
                          locked={false}
                          sp={null}
                          choice={arenaChoice}
                          inCombat={false}
                          cardId={cardId}
                          target={target(s.id, `arena:${s.id}`)}
                          traitTarget={(k) => target(s.id, `arena-trait:${s.id}:${k}`, k)}
                        />
                      ),
                    )}
                  </ul>
                </section>
              ))}
              <h3 className="skill-class">Arena bar</h3>
              <ArenaBar cls={cls} bar={bar} />
            </>
          )
        ) : (
          <>
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
              <p className="skill-text muted skill-combat">
                In combat: traits change once out of it.
              </p>
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
          </>
        )}
      </div>
      {shown && card && (
        <BesidePointer at={shown.at}>
          {shown.trait === undefined ? (
            <SkillCard
              id={cardId}
              cls={cls}
              skill={card}
              points={pointsIn(card, view.invested)}
              staged={arena ? 0 : (sp.staged[card.id] ?? 0)}
              invested={view.invested}
              trait={view.choice.chosen(card)}
              locked={lockedNow(card)}
              clock={clock}
            />
          ) : (
            <TraitCard
              id={cardId}
              skill={card}
              trait={shown.trait}
              points={pointsIn(card, view.invested)}
              chosen={card.traits[shown.trait]?.id === view.choice.chosen(card)}
              inCombat={view.inCombat}
              clock={clock}
            />
          )}
        </BesidePointer>
      )}
    </WorldWindow>
  );
}
