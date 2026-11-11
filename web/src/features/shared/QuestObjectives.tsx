import { isReady, npcById, objectiveCount, objectiveLabel } from '@/progression/quests';
import type { Player, Quest } from '@/progression/quests';

/** An active quest's objectives with their counts, items from the inventory. */
export function QuestObjectives({
  quest,
  player,
}: {
  readonly quest: Quest;
  readonly player: Player;
}) {
  const entry = player.log[quest.id];
  if (!entry) {
    return null;
  }
  return (
    <ul className="quest-objectives" aria-label={`${quest.name} objectives`}>
      {quest.objectives.map((o, k) => {
        const n = Math.min(objectiveCount(quest, entry, k, player), o.count);
        const done = n >= o.count;
        return (
          <li key={k} className={done ? 'quest-done' : ''}>
            {o.kind === 'level' ? objectiveLabel(o) : `${objectiveLabel(o)} ${n}/${o.count}`}
            {done && ' ✓'}
          </li>
        );
      })}
      {isReady(quest, player) && (
        <li className="quest-ready">
          Ready to hand in to {npcById(quest.turn_in)?.name ?? quest.turn_in}
        </li>
      )}
    </ul>
  );
}
