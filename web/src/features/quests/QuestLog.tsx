// The active quests with their objectives; QuestChanged, not the Response, takes an abandoned one out.
import { useState } from 'react';

import { Button } from '@/components/Button';
import { problemOf } from '@/features/shared/problem';
import { QuestObjectives } from '@/features/shared/QuestObjectives';
import { useQuestPlayer } from '@/features/shared/useQuestPlayer';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { questById } from '@/progression/quests';
import type { Quest } from '@/progression/quests';

export interface QuestLogProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

export function QuestLog({ socket, onClose }: QuestLogProps) {
  const player = useQuestPlayer();
  const [confirming, setConfirming] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const entries = Object.entries(player.log);
  const active = entries
    .flatMap(([id, e]) => (e.status === 'active' ? (questById(id) ?? []) : []))
    .sort((a, b) => a.name.localeCompare(b.name));
  const finished = entries.filter(([, e]) => e.timesFinished > 0).length;

  const abandon = async (q: Quest) => {
    setBusy(true);
    setProblem(null);
    try {
      await socket.request('abandonQuest', { questId: q.id });
      setConfirming(null);
    } catch (err) {
      setProblem(problemOf(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <WorldWindow name="quests" title="Quests" onClose={onClose} initialX={940} initialY={120}>
      <div className="quest-log">
        {active.length === 0 ? (
          <p className="muted">No quests under way. Talk to the people in town.</p>
        ) : (
          <ul className="npc-quests" aria-label="Active quests">
            {active.map((q) => (
              <li key={q.id}>
                <div className="npc-quest-head">
                  <strong>{q.name}</strong>
                  {confirming !== q.id && (
                    <Button variant="quiet" disabled={busy} onClick={() => setConfirming(q.id)}>
                      Abandon
                    </Button>
                  )}
                </div>
                <QuestObjectives quest={q} player={player} />
                {confirming === q.id && (
                  <div className="row">
                    <span>Abandon {q.name}? Its progress is lost.</span>
                    <Button variant="danger" disabled={busy} onClick={() => void abandon(q)}>
                      Abandon
                    </Button>
                    <Button variant="quiet" disabled={busy} onClick={() => setConfirming(null)}>
                      Keep
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
        {finished > 0 && <p className="muted">Quests finished: {finished}</p>}
        {problem && (
          <p className="error" role="alert">
            {problem}
          </p>
        )}
      </div>
    </WorldWindow>
  );
}
