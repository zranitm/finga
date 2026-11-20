// The arena bar under the skill window's Arena tab (pvp.md section 13): eight keys, each labelled with the key it's bound to. A skill dragged from the tab goes on a key, a key dragged onto another swaps them, and dropping it off the bar empties it.
import { useEffect, useRef, useState } from 'react';
import type { DragEvent } from 'react';

import { dragTile, SkillIcon } from '@/components/SkillIcon';
import { takeDropsOff } from '@/features/hud/SkillBar';
import { useKeys } from '@/features/shared/useKeys';
import { skillKeys } from '@/input/actions';
import { keyable } from '@/progression/chains';
import { fullPoints, pointsIn, skillById, specAt } from '@/progression/skills';
import { arenaBuild } from '@/state/arena-build';
import { SKILL_DRAG_TYPE } from '@/state/skill-bar';

/** What an arena key carries when dragged: its index; apart from the skill bar's, so neither bar takes the other's keys. */
const ARENA_KEY_TYPE = 'application/x-finga-arena-key';

/** How long a key flashes red at a follow-up dropped on it, as the skill bar's does. */
const REFUSED_MS = 400;

export function ArenaBar({ cls, bar }: { readonly cls: string; readonly bar: readonly string[] }) {
  const [refused, setRefused] = useState<{ readonly key: number; readonly n: number } | null>(null);
  useEffect(() => {
    if (!refused) {
      return undefined;
    }
    const id = setTimeout(() => setRefused(null), REFUSED_MS);
    return () => clearTimeout(id);
  }, [refused]);
  const onDrop = (e: DragEvent, key: number) => {
    e.preventDefault();
    const skill = e.dataTransfer.getData(SKILL_DRAG_TYPE);
    const from = e.dataTransfer.getData(ARENA_KEY_TYPE);
    if (skill !== '' && !keyable(cls, skill)) {
      setRefused((r) => ({ key, n: (r?.n ?? 0) + 1 })); // a follow-up rides on its root's key
    } else if (skill !== '') {
      arenaBuild.getState().put(key, skill);
    } else if (from !== '' && Number(from) !== key) {
      arenaBuild.getState().swap(Number(from), key);
    }
  };
  return (
    <ol className="skill-bar arena-bar" aria-label="Arena bar">
      {bar.map((skill, key) => (
        <ArenaKey
          key={key}
          index={key}
          cls={cls}
          skill={skill}
          refused={refused?.key === key ? refused.n : 0}
          onDrop={onDrop}
        />
      ))}
    </ol>
  );
}

function ArenaKey({
  index,
  cls,
  skill,
  refused,
  onDrop,
}: {
  readonly index: number;
  readonly cls: string;
  readonly skill: string;
  readonly refused: number;
  readonly onDrop: (e: DragEvent, key: number) => void;
}) {
  const trait = arenaBuild.use((s) => s.traits[skill]);
  const data = skill === '' ? undefined : skillById(skill);
  const points = data ? pointsIn(data, fullPoints(cls)) : 0;
  const stage = data && specAt(data, points, trait);
  const dragging = useRef<(() => void) | null>(null);
  useEffect(() => () => dragging.current?.(), []);
  const keys = useKeys();
  const bound = keys.name(skillKeys[index]!);
  const label = `Key ${bound}: ${stage ? stage.name : 'empty'}`;
  return (
    <li
      className={`skill-key${stage ? '' : ' skill-key-empty'}`}
      aria-label={label}
      title={stage ? label : `${label}. Drag a skill here from the grid above.`}
      draggable={stage !== undefined}
      onDragStart={(e) => {
        e.dataTransfer.setData(ARENA_KEY_TYPE, String(index));
        e.dataTransfer.effectAllowed = 'move';
        dragTile(e);
        dragging.current = takeDropsOff('.arena-bar', () => arenaBuild.getState().clear(index));
      }}
      onDragEnd={() => {
        dragging.current?.();
        dragging.current = null;
      }}
      onDragOver={(e) => {
        e.preventDefault();
        // A drop needs an effect the drag allows: a skill from the grid copies, a key moves.
        e.dataTransfer.dropEffect = e.dataTransfer.types.includes(SKILL_DRAG_TYPE)
          ? 'copy'
          : 'move';
      }}
      onDrop={(e) => onDrop(e, index)}
    >
      {data && stage && <SkillIcon skill={data} points={points} size="bar" />}
      <span className="skill-key-bound">{keys.short(skillKeys[index]!)}</span>
      {refused > 0 && <span key={refused} className="skill-key-refused" aria-hidden="true" />}
    </li>
  );
}
