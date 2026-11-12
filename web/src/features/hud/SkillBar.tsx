// Keys 1 to 6: a skill dragged from the skill window goes on a key, dragging a key onto another swaps them, and dragging it off the bar empties it.
import type { DragEvent } from 'react';

import { mpAt, skillById, stageAt } from '@/progression/skills';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import type { Cooldown } from '@/state/skill-bar';

/** What a key carries when dragged: its index. */
const KEY_TYPE = 'application/x-finga-skill-key';

export function SkillBar() {
  const slots = skillBar.use((s) => s.slots);
  const cooldowns = skillBar.use((s) => s.cooldowns);
  const onDrop = (e: DragEvent, key: number) => {
    e.preventDefault();
    const skill = e.dataTransfer.getData(SKILL_DRAG_TYPE);
    const from = e.dataTransfer.getData(KEY_TYPE);
    if (skill !== '') {
      skillBar.getState().put(key, skill);
    } else if (from !== '' && Number(from) !== key) {
      skillBar.getState().swap(Number(from), key);
    }
  };
  return (
    <ol className="skill-bar" aria-label="Skill bar">
      {slots.map((id, key) => (
        <SkillKey key={key} index={key} skill={id} cooldown={cooldowns[id]} onDrop={onDrop} />
      ))}
    </ol>
  );
}

function SkillKey({
  index,
  skill,
  cooldown,
  onDrop,
}: {
  readonly index: number;
  readonly skill: string;
  readonly cooldown: Cooldown | undefined;
  readonly onDrop: (e: DragEvent, key: number) => void;
}) {
  const points = character.use((s) => s.character?.sheet?.skills?.invested[skill] ?? 0);
  const mp = character.use((s) => s.character?.mp ?? 0);
  const data = skill === '' ? undefined : skillById(skill);
  const stage = data?.stages[stageAt(data, points)];
  const cost = stage ? mpAt(stage, points) : 0;
  const usable = stage !== undefined && points > 0 && mp >= cost;
  const label = `Key ${index + 1}: ${stage ? `${stage.name}${points > 0 ? `, ${cost} MP` : ', no points yet'}` : 'empty'}`;
  return (
    <li
      className={`skill-key${stage && !usable ? ' skill-key-off' : ''}`}
      aria-label={label}
      title={stage ? label : `${label}. Drag a skill here from the skill window (K).`}
      draggable={stage !== undefined}
      onDragStart={(e) => {
        e.dataTransfer.setData(KEY_TYPE, String(index));
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragEnd={(e) => {
        if (e.dataTransfer.dropEffect === 'none') {
          skillBar.getState().clear(index);
        }
      }}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
      }}
      onDrop={(e) => onDrop(e, index)}
    >
      <span className="skill-key-number">{index + 1}</span>
      {stage && <span className="skill-key-name">{stage.name}</span>}
      {cooldown && (
        // Mounted as the cooldown starts, it drains over the whole cooldown; the cooldown's end removes it.
        <span
          key={cooldown.from}
          className="skill-key-cooldown"
          style={{ animationDuration: `${cooldown.until - cooldown.from}ms` }}
        />
      )}
    </li>
  );
}
