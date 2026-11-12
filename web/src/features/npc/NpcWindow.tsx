// What an NPC offers, as its NpcDialog said when talked to: quests, and a trainer's promotion; what it takes back is ready by the live log and inventory.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { Modal } from '@/components/Modal';
import { itemName } from '@/features/shared/items';
import { problemOf } from '@/features/shared/problem';
import { QuestObjectives } from '@/features/shared/QuestObjectives';
import { useQuestPlayer } from '@/features/shared/useQuestPlayer';
import { article } from '@/features/shared/words';
import { WorldWindow } from '@/features/shared/WorldWindow';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import { classDescription, className } from '@/progression/classes';
import { isReady, npcById, questById, questName } from '@/progression/quests';
import type { Quest } from '@/progression/quests';
import { skillById } from '@/progression/skills';
import { PromotionConditionKind, QuestOfferState } from '@/protocol/messages';
import type {
  PromotionCondition,
  PromotionOffer,
  QuestConditions,
  QuestOffer,
} from '@/protocol/messages';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { inventory } from '@/state/inventory';
import type { Tab } from '@/state/inventory';
import { quests } from '@/state/quests';

import { haveNow } from './promotion';
import { fitsNow, freeText, fullTab, lineOf, toFree, trainerWeapon } from './room';
import type { Line } from './room';

export interface NpcWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

export function NpcWindow({ socket, onClose }: NpcWindowProps) {
  const dialog = quests.use((s) => s.dialog);
  const player = useQuestPlayer();
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!dialog) {
    return null;
  }
  const npc = dialog.npcId;

  // Accept, Hand in and Promote close the dialog on success: what it showed has changed.
  const run = async (send: () => Promise<unknown>, room?: (full: Tab) => string) => {
    setBusy(true);
    setProblem(null);
    try {
      await send();
      onClose();
    } catch (err) {
      const full = err instanceof RequestError ? fullTab(err.code) : undefined;
      setProblem(full && room ? room(full) : problemOf(err));
    } finally {
      setBusy(false);
    }
  };

  // A reward that doesn't fit now isn't asked for: the player is told what to free, counting their drops waiting for room, which can take a freed slot first.
  const exchange = (
    take: readonly Line[],
    give: readonly Line[],
    what: string,
    send: () => Promise<unknown>,
  ) => {
    const room = (full?: Tab) => {
      const waiting = combatLog
        .getState()
        .waiting.flatMap((w) => lineOf(w.template, w.quantity) ?? []);
      const short = toFree(inventory.getState().slots, take, give, waiting);
      return freeText(Object.keys(short).length > 0 ? short : { [full ?? 'equip']: 1 }, what);
    };
    if (!fitsNow(inventory.getState().slots, take, give)) {
      setProblem(room());
      return;
    }
    void run(send, room);
  };

  const lines = npcById(npc)?.lines ?? [];
  const takes = dialog.takes.flatMap((t) => questById(t.questId) ?? []);
  // A quest under way that this NPC also takes back shows once, under Hand in.
  const gives = dialog.gives.filter(
    (o) => o.state !== QuestOfferState.ACTIVE || !takes.some((q) => q.id === o.questId),
  );
  return (
    <WorldWindow
      name="npc"
      title={npcById(npc)?.name ?? npc}
      onClose={onClose}
      initialX={610}
      initialY={120}
    >
      <div className="npc-dialog">
        {lines.map((line) => (
          <p key={line} className="npc-line">
            {line}
          </p>
        ))}
        {lines.length === 0 && gives.length === 0 && takes.length === 0 && !dialog.promotion && (
          <p className="muted">Nothing for you right now.</p>
        )}
        {dialog.promotion && (
          <Promotion
            offer={dialog.promotion}
            busy={busy}
            onPromote={(cls) =>
              exchange([], [trainerWeapon], 'your weapon', () =>
                socket.request('promote', { npcId: npc, class: cls }),
              )
            }
          />
        )}
        {takes.length > 0 && (
          <>
            <h3>Hand in</h3>
            <ul className="npc-quests" aria-label="Quests to hand in">
              {takes.map((q) => (
                <li key={q.id}>
                  <div className="npc-quest-head">
                    <strong>{q.name}</strong>
                    <Button
                      disabled={busy || !isReady(q, player)}
                      onClick={() => {
                        const { take, give } = exchanged(q);
                        exchange(take, give, 'this reward', () =>
                          socket.request('handInQuest', { npcId: npc, questId: q.id }),
                        );
                      }}
                    >
                      Hand in
                    </Button>
                  </div>
                  <QuestObjectives quest={q} player={player} />
                  <Rewards quest={q} />
                </li>
              ))}
            </ul>
          </>
        )}
        {gives.length > 0 && (
          <>
            <h3>Quests</h3>
            <ul className="npc-quests" aria-label="Quests offered">
              {gives.map((o) => (
                <Offer
                  key={o.questId}
                  offer={o}
                  busy={busy}
                  onAccept={() =>
                    void run(() =>
                      socket.request('acceptQuest', { npcId: npc, questId: o.questId }),
                    )
                  }
                />
              ))}
            </ul>
          </>
        )}
        {problem && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}
      </div>
    </WorldWindow>
  );
}

function Promotion({
  offer,
  busy,
  onPromote,
}: {
  readonly offer: PromotionOffer;
  readonly busy: boolean;
  readonly onPromote: (cls: string) => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const me = character.use((s) => s.character);
  const log = quests.use((s) => s.log);
  // The dialog's counts are from the talk; spends, level-ups and quests since update them here, and the world checks again.
  const conditions = me
    ? offer.conditions.map((c) => ({
        ...c,
        have: haveNow(c, {
          level: me.level,
          own: me.stats,
          invested: me.sheet?.skills?.invested ?? {},
          log,
        }),
      }))
    : offer.conditions;
  const name = className(offer.class);
  const become = `Become ${article(name)} ${name}`;
  const met = conditions.every((c) => c.have >= c.need);
  return (
    <section className="npc-promotion" aria-label="Promotion">
      <h3>{become}</h3>
      <p className="npc-line">{classDescription(offer.class)}</p>
      <ul className="quest-objectives" aria-label="Promotion conditions">
        {conditions.map((c) => (
          <li key={`${c.kind}:${c.id}`} className={c.have >= c.need ? 'quest-done' : ''}>
            {conditionText(c)}
            {c.have >= c.need && ' ✓'}
          </li>
        ))}
      </ul>
      <Button variant="primary" disabled={busy || !met} onClick={() => setConfirming(true)}>
        {become}
      </Button>
      {confirming && (
        <Modal title={`${become}?`} onClose={() => setConfirming(false)}>
          <p>
            A promotion is for good: you stay {article(name)} {name} from now on. The trainer hands
            you your first weapon.
          </p>
          <Button
            variant="primary"
            disabled={busy}
            onClick={() => {
              setConfirming(false);
              onPromote(offer.class);
            }}
          >
            {become}
          </Button>
          <Button variant="quiet" onClick={() => setConfirming(false)}>
            Not yet
          </Button>
        </Modal>
      )}
    </section>
  );
}

/** A promotion condition, with what the character has while it falls short. */
export function conditionText(c: PromotionCondition): string {
  const short = c.have < c.need;
  switch (c.kind) {
    case PromotionConditionKind.LEVEL:
      return `Level ${c.need}${short ? ` · you're ${c.have}` : ''}`;
    case PromotionConditionKind.STAT:
      return `${c.id.toUpperCase()} ${c.need} without gear${short ? ` · yours ${c.have}` : ''}`;
    case PromotionConditionKind.SKILL: {
      const skill = skillById(c.id)?.stages[0]?.name ?? c.id;
      return `${c.need} points in ${skill}${short ? ` · you have ${c.have}` : ''}`;
    }
    case PromotionConditionKind.QUEST:
      return `${questName(c.id)} finished${short ? ' · not yet' : ''}`;
    default:
      return "A condition this client doesn't know";
  }
}

function Offer({
  offer,
  busy,
  onAccept,
}: {
  readonly offer: QuestOffer;
  readonly busy: boolean;
  readonly onAccept: () => void;
}) {
  const q = questById(offer.questId);
  const name = q?.name ?? offer.questId;
  switch (offer.state) {
    case QuestOfferState.AVAILABLE:
      return (
        <li>
          <div className="npc-quest-head">
            <strong>{name}</strong>
            <Button disabled={busy} onClick={onAccept}>
              Accept
            </Button>
          </div>
          {q && <Rewards quest={q} />}
        </li>
      );
    case QuestOfferState.ACTIVE:
      return (
        <li className="npc-quest-head">
          <strong>{name}</strong>
          <span className="muted">In progress</span>
        </li>
      );
    case QuestOfferState.FINISHED:
      return (
        <li className="npc-quest-head">
          <strong>{name}</strong>
          <span className="muted">Done</span>
        </li>
      );
    default:
      return (
        <li className="npc-quest-unmet" aria-disabled="true">
          <strong>{name}</strong>
          <span className="muted">{unmetText(offer.unmet)}</span>
        </li>
      );
  }
}

/** Why a quest can't be taken yet, from what the server says is missing. */
export function unmetText(u: QuestConditions | undefined): string {
  const parts: string[] = [];
  if (u?.level) {
    parts.push(`Needs level ${u.level}`);
  }
  if (u?.class) {
    parts.push(`${className(u.class)} only`);
  }
  if (u && u.quests.length > 0) {
    parts.push(`Finish ${u.quests.map(questName).join(', ')} first`);
  }
  return parts.length > 0 ? parts.join(' · ') : 'Not available yet';
}

/** What a hand-in takes, its collect objectives, and gives, its item rewards, as the world asks the item service for them. */
function exchanged(q: Quest): { take: Line[]; give: Line[] } {
  return {
    take: q.objectives.flatMap((o) =>
      o.kind === 'collect' && o.target ? (lineOf(o.target, o.count) ?? []) : [],
    ),
    give: q.rewards.items.flatMap((i) => lineOf(i.item, i.quantity) ?? []),
  };
}

function Rewards({ quest }: { readonly quest: Quest }) {
  const { exp, items } = quest.rewards;
  const parts = [
    ...(exp > 0 ? [`${exp} EXP`] : []),
    ...items.map((i) => `${itemName(i.item)}${i.quantity > 1 ? ` ×${i.quantity}` : ''}`),
  ];
  return parts.length > 0 ? <p className="npc-rewards">Rewards: {parts.join(', ')}</p> : null;
}
