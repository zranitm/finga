// One frame look for the player, their party and their target (target-lock.md section 13): the HUD's panel, a stripe and a mark for the side, a header, and the HP bar.
import type { ReactNode } from 'react';

import { Bar } from '@/components/Bar';

export type Side = 'ally' | 'enemy';

export interface UnitFrameProps {
  readonly side: Side;
  readonly name: string;
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  /** A tag in the header, such as Elite. */
  readonly badge?: string;
  /** The frame's accessible name, saying whose it is. */
  readonly label: string;
  /** The HP bar's accessible name, where several frames share the screen. */
  readonly hpName?: string;
  /** The party panel's size. */
  readonly compact?: boolean;
  readonly dimmed?: boolean;
  /** Rows under the HP bar. */
  readonly children?: ReactNode;
}

/** The side's mark: a dot for an ally, a cross for an enemy, so the side reads without colour. */
export function SideMark({ side }: { readonly side: Side }) {
  return (
    <span className="side-mark" data-side={side} aria-hidden="true">
      {side === 'ally' ? '●' : '✕'}
    </span>
  );
}

export function UnitFrame({
  side,
  name,
  level,
  hp,
  maxHp,
  badge,
  label,
  hpName,
  compact,
  dimmed,
  children,
}: UnitFrameProps) {
  const classes = [
    'unit-frame',
    `side-${side}`,
    compact && 'unit-frame-compact',
    dimmed && 'unit-frame-dimmed',
  ];
  return (
    <div className={classes.filter(Boolean).join(' ')} aria-label={label}>
      <div className="unit-frame-head">
        <SideMark side={side} />
        <strong className="unit-frame-name" title={name}>
          {name}
        </strong>
        {badge && <span className="unit-frame-badge">{badge}</span>}
        <span className="unit-frame-level">Lv. {level}</span>
      </div>
      <Bar label="HP" value={hp} max={maxHp} kind="hp" {...(hpName && { name: hpName })} />
      {children}
    </div>
  );
}
