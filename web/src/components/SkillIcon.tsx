// A skill's icon tile (skill-icons.md sections 4 and 5): its glyph in white on the skill's colour, its stage in pips, and a cooldown's sweep and count.
import { useCallback, useEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';

import { colorOf } from '@/progression/skills';
import type { Skill } from '@/progression/skills';

const FLASH_MS = 150;
/** The store ends a cooldown by a timer that may beat the last frame: leaving within this of its end is ending. */
const END_SLACK_MS = 50;

/** A cooldown in performance.now() milliseconds. */
export interface Cooling {
  readonly from: number;
  readonly until: number;
}

export interface SkillIconProps {
  readonly skill: Skill;
  /** The stage's index. */
  readonly stage: number;
  readonly size: 'bar' | 'window';
  /** Short of energy or of points: greyed. */
  readonly off?: boolean;
  readonly cooldown?: Cooling | undefined;
}

export function SkillIcon({ skill, stage, size, off = false, cooldown }: SkillIconProps) {
  const st = skill.stages[stage]!;
  const [flashed, setFlashed] = useState(0);
  useEffect(() => {
    if (flashed === 0) {
      return undefined;
    }
    const id = setTimeout(() => setFlashed(0), FLASH_MS);
    return () => clearTimeout(id);
  }, [flashed]);
  const ready = useCallback(() => setFlashed(performance.now()), []);
  const style = {
    '--skill': `#${colorOf(skill, st).toString(16).padStart(6, '0')}`,
    '--icon': `url("${import.meta.env.BASE_URL}icons/skills/${st.icon}.svg")`,
  } as CSSProperties;
  return (
    <span
      className={`skill-icon skill-icon-${size}${off ? ' skill-icon-off' : ''}`}
      style={style}
      aria-hidden="true"
    >
      <span className="skill-icon-glyph" />
      {skill.stages.length > 1 && (
        <span className="skill-icon-pips">
          {skill.stages.slice(0, stage + 1).map((s) => (
            <span key={s.at} className="skill-icon-pip" />
          ))}
        </span>
      )}
      {cooldown && (
        <Sweep key={`${cooldown.from}:${cooldown.until}`} cooldown={cooldown} onEnd={ready} />
      )}
      {flashed > 0 && <span key={flashed} className="skill-icon-flash" />}
    </span>
  );
}

/** The shade and count from where the cooldown stands when mounted, so one restored part-way starts part-way; onEnd at its end. */
function Sweep({ cooldown, onEnd }: { readonly cooldown: Cooling; readonly onEnd: () => void }) {
  const shade = useRef<HTMLSpanElement>(null);
  const [left] = useState(() => leftOf(cooldown, performance.now()));
  const [count, setCount] = useState(() => countOf(cooldown.until - performance.now()));
  useEffect(() => {
    let frame = 0;
    let ended = false;
    const step = () => {
      const now = performance.now();
      if (now >= cooldown.until) {
        ended = true;
        onEnd();
        return;
      }
      shade.current?.style.setProperty('--left', `${leftOf(cooldown, now)}deg`);
      setCount(countOf(cooldown.until - now));
      frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => {
      cancelAnimationFrame(frame);
      if (!ended && performance.now() >= cooldown.until - END_SLACK_MS) {
        onEnd();
      }
    };
  }, [cooldown, onEnd]);
  return (
    <>
      <span
        ref={shade}
        className="skill-icon-sweep"
        style={{ '--left': `${left}deg` } as CSSProperties}
      />
      <span className="skill-icon-count">{count}</span>
    </>
  );
}

/** The shade's angle: 360° at the start, 0° at the end. */
function leftOf({ from, until }: Cooling, now: number): number {
  return 360 * Math.min(1, Math.max(0, (until - now) / (until - from)));
}

/** Whole seconds rounded up, and tenths rounded up below one. */
function countOf(leftMs: number): string {
  const ms = Math.max(0, leftMs);
  return ms >= 1000 ? String(Math.ceil(ms / 1000)) : (Math.ceil(ms / 100) / 10).toFixed(1);
}
