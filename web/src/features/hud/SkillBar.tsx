// Keys 1 to 6: a skill dragged from the skill window goes on a key, dragging a key onto another swaps them, and dropping it off the bar empties it.
import { useEffect, useRef, useState } from 'react';
import type { DragEvent } from 'react';

import { mpAt, skillById, stageAt } from '@/progression/skills';
import { character } from '@/state/character';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import type { Cooldown } from '@/state/skill-bar';

/** What a key carries when dragged: its index. */
const KEY_TYPE = 'application/x-finga-skill-key';

/** While key index is dragged, the whole page takes it, so a drop anywhere off the bar empties the key; Esc drops nowhere and keeps it. Returns the end of that. */
function takeDropsOff(index: number): () => void {
  const over = (e: globalThis.DragEvent) => {
    e.preventDefault();
  };
  const drop = (e: globalThis.DragEvent) => {
    e.preventDefault();
    if (!(e.target instanceof Element && e.target.closest('.skill-bar'))) {
      skillBar.getState().clear(index);
    }
  };
  document.addEventListener('dragover', over);
  document.addEventListener('drop', drop);
  return () => {
    document.removeEventListener('dragover', over);
    document.removeEventListener('drop', drop);
  };
}

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
  const dragging = useRef<(() => void) | null>(null);
  useEffect(() => () => dragging.current?.(), []);
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
        dragging.current = takeDropsOff(index);
      }}
      onDragEnd={() => {
        dragging.current?.();
        dragging.current = null;
      }}
      onDragOver={(e) => {
        e.preventDefault();
        // A drop needs an effect the drag allows: the skill window's rows copy, a key moves.
        e.dataTransfer.dropEffect = e.dataTransfer.types.includes(SKILL_DRAG_TYPE)
          ? 'copy'
          : 'move';
      }}
      onDrop={(e) => onDrop(e, index)}
    >
      <span className="skill-key-number">{index + 1}</span>
      {stage && <span className="skill-key-name">{stage.name}</span>}
      {cooldown && <CooldownShade key={cooldown.from} cooldown={cooldown} />}
    </li>
  );
}

/** Drains over the whole cooldown from where it stands when mounted, so one a Welcome restored part-way through starts part-way; the cooldown's end removes it. */
function CooldownShade({ cooldown }: { readonly cooldown: Cooldown }) {
  const [elapsed] = useState(() => Math.max(0, performance.now() - cooldown.from));
  return (
    <span
      className="skill-key-cooldown"
      style={{
        animationDuration: `${cooldown.until - cooldown.from}ms`,
        animationDelay: `${-elapsed}ms`,
      }}
    />
  );
}
