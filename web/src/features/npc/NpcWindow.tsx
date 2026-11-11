// What an NPC offers, as its NpcDialog said when talked to; what it takes back is ready by the live log and inventory.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { itemName } from '@/features/shared/items';
import { problemOf } from '@/features/shared/problem';
import { QuestObjectives } from '@/features/shared/QuestObjectives';
import { useQuestPlayer } from '@/features/shared/useQuestPlayer';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { className } from '@/progression/classes';
import { isReady, npcById, questById, questName } from '@/progression/quests';
import type { Quest } from '@/progression/quests';
import { QuestOfferState } from '@/protocol/messages';
import type { QuestConditions, QuestOffer } from '@/protocol/messages';
import { quests } from '@/state/quests';

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

  // Accept and Hand in close the dialog on success: what it showed has changed.
  const run = async (send: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(null);
    try {
      await send();
      onClose();
    } catch (err) {
      setProblem(problemOf(err));
    } finally {
      setBusy(false);
    }
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
      initialX={500}
      initialY={120}
    >
      <div className="npc-dialog">
        {lines.map((line) => (
          <p key={line} className="npc-line">
            {line}
          </p>
        ))}
        {lines.length === 0 && gives.length === 0 && takes.length === 0 && (
          <p className="muted">Nothing for you right now.</p>
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
                      onClick={() =>
                        void run(() => socket.request('handInQuest', { npcId: npc, questId: q.id }))
                      }
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

function Rewards({ quest }: { readonly quest: Quest }) {
  const { exp, items } = quest.rewards;
  const parts = [
    ...(exp > 0 ? [`${exp} EXP`] : []),
    ...items.map((i) => `${itemName(i.item)}${i.quantity > 1 ? ` ×${i.quantity}` : ''}`),
  ];
  return parts.length > 0 ? <p className="npc-rewards">Rewards: {parts.join(', ')}</p> : null;
}
