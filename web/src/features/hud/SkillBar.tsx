// The bar's eight keys, each labelled with the key it's bound to: a skill dragged from the skill window goes on a key, dragging a key onto another swaps them, and dropping it off the bar empties it.
// A key holds a chain's root or a skill in no chain, and shows what it offers now: a live follow-up in a frame glowing its colour (skill-chains.md section 5).
import { useEffect, useRef, useState } from 'react';
import type { CSSProperties, DragEvent } from 'react';

import { dragTile, SkillIcon } from '@/components/SkillIcon';
import { useKeys } from '@/features/shared/useKeys';
import { skillKeys } from '@/input/actions';
import { conditionText, followUpOf, keyable } from '@/progression/chains';
import { colorOf, pointsIn, skillById, specAt, stageAt } from '@/progression/skills';
import { character } from '@/state/character';
import { energy } from '@/state/energy';
import { SKILL_DRAG_TYPE, skillBar } from '@/state/skill-bar';
import type { Cooldown } from '@/state/skill-bar';

/** What a key carries when dragged: its index. */
const KEY_TYPE = 'application/x-finga-skill-key';

/** How long a key flashes red at a follow-up dropped on it. */
const REFUSED_MS = 400;

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
  const live = skillBar.use((s) => s.live);
  const cooldowns = skillBar.use((s) => s.cooldowns);
  const cls = character.use((s) => s.character?.class ?? '');
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
    const from = e.dataTransfer.getData(KEY_TYPE);
    if (skill !== '' && !keyable(cls, skill)) {
      setRefused((r) => ({ key, n: (r?.n ?? 0) + 1 })); // a follow-up rides on its root's key
    } else if (skill !== '') {
      skillBar.getState().put(key, skill);
    } else if (from !== '' && Number(from) !== key) {
      skillBar.getState().swap(Number(from), key);
    }
  };
  return (
    <ol className="skill-bar" aria-label="Skill bar">
      {slots.map((root, key) => {
        const id = live[key] === '' ? root : (live[key] ?? root);
        return (
          <SkillKey
            key={key}
            index={key}
            cls={cls}
            root={root}
            skill={id}
            cooldown={cooldowns[id]}
            refused={refused?.key === key ? refused.n : 0}
            onDrop={onDrop}
          />
        );
      })}
    </ol>
  );
}

/** A key's tooltip for a live follow-up: on whose key it comes, the root named at its stage, and while what holds. */
function chainText(cls: string, root: string, rootPoints: number, skill: string): string {
  const when = followUpOf(cls, root, skill)?.when;
  const rootData = skillById(root);
  const rootName = rootData?.stages[stageAt(rootData, rootPoints)]?.name ?? root;
  return when ? `, on ${rootName}’s key ${conditionText(when, rootName)}` : '';
}

function SkillKey({
  index,
  cls,
  root,
  skill,
  cooldown,
  refused,
  onDrop,
}: {
  readonly index: number;
  readonly cls: string;
  /** The skill on the key; skill is what it offers now, the same or a follow-up of root's. */
  readonly root: string;
  readonly skill: string;
  readonly cooldown: Cooldown | undefined;
  /** Counts the follow-ups dropped on it and refused while it flashes red, 0 for none. */
  readonly refused: number;
  readonly onDrop: (e: DragEvent, key: number) => void;
}) {
  const invested = character.use((s) => s.character?.sheet?.skills?.invested);
  const trait = character.use((s) => s.character?.sheet?.skills?.traits[skill]);
  const data = skill === '' ? undefined : skillById(skill);
  const points = data ? pointsIn(data, invested ?? {}) : 0;
  const rootPoints = invested?.[root] ?? 0;
  const shown = energy.use((s) => s.shown);
  const stage = data && specAt(data, points, trait);
  const extra = data?.traits[1];
  const withExtra = extra !== undefined && stage === extra.stage; // the stage the world uses is the extra's
  const cost = stage?.energy ?? 0;
  const usable = stage !== undefined && points > 0 && shown >= cost;
  const dragging = useRef<(() => void) | null>(null);
  useEffect(() => () => dragging.current?.(), []);
  const chained = data !== undefined && stage !== undefined && skill !== root;
  const keys = useKeys();
  const bound = keys.name(skillKeys[index]!);
  const short = keys.short(skillKeys[index]!);
  const label = `Key ${bound}: ${stage ? `${stage.name}${withExtra ? ` with ${extra.name}` : ''}${points > 0 ? `, ${cost} energy` : ', no points yet'}` : 'empty'}${chained ? chainText(cls, root, rootPoints, skill) : ''}`;
  const color = data && stage ? `#${colorOf(data, stage).toString(16).padStart(6, '0')}` : '';
  const style =
    chained || withExtra
      ? ({
          ...(chained && { '--chain': color }),
          ...(withExtra && { '--skill': color }),
        } as CSSProperties)
      : undefined;
  return (
    <li
      className={`skill-key${stage ? (usable ? '' : ' skill-key-off') : ' skill-key-empty'}${chained ? ' skill-key-chain' : ''}`}
      style={style}
      aria-label={label}
      title={
        stage
          ? label
          : `${label}. Drag a skill here from the skill window (${keys.name('skills')}).`
      }
      draggable={stage !== undefined}
      onDragStart={(e) => {
        e.dataTransfer.setData(KEY_TYPE, String(index));
        e.dataTransfer.effectAllowed = 'move';
        dragTile(e);
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
      {data && stage && (
        <SkillIcon skill={data} points={points} size="bar" off={!usable} cooldown={cooldown} />
      )}
      <span className={`skill-key-bound${short.length > 4 ? ' skill-key-bound-long' : ''}`}>
        {short}
      </span>
      {refused > 0 && <span key={refused} className="skill-key-refused" aria-hidden="true" />}
      {stage && cost > 0 && (
        <span className="skill-key-cost" aria-hidden="true">
          {cost}
        </span>
      )}
      {withExtra && (
        <svg className="skill-key-trait" viewBox="0 0 12 12" aria-hidden="true">
          <polygon points="6,1 11,6 6,11 1,6" />
        </svg>
      )}
    </li>
  );
}
