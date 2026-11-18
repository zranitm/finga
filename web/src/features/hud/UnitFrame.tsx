// One frame look for the player, their party and their target (target-lock.md section 13): the HUD's panel, a stripe and a mark for the side, a header, and the HP bar.
import type { ReactNode } from 'react';

import { Bar } from '@/components/Bar';
import type { FrameBuff } from '@/state/character';
import type { ShownStatus } from '@/state/statuses';

import { FrameIcons } from './FrameIcons';
import type { HiddenCooldown } from './hiddenCooldowns';

export type Side = 'ally' | 'enemy';

export interface UnitFrameProps {
  readonly side: Side;
  readonly name: string;
  readonly level: number;
  readonly hp: number;
  readonly maxHp: number;
  /** What's left of their shields, shown pale over the HP bar. */
  readonly shield?: number;
  /** A tag in the header, such as Elite. */
  readonly badge?: string;
  /** The frame's accessible name, saying whose it is. */
  readonly label: string;
  /** The HP bar's accessible name, where several frames share the screen. */
  readonly hpName?: string;
  /** compact for the party rows and the target window: smaller text, a thin HP bar with its numbers over it. */
  readonly size?: 'full' | 'compact';
  readonly dimmed?: boolean;
  /** Its statuses, as icons right under the HP bar. */
  readonly statuses?: readonly ShownStatus[];
  /** The player's own frame's: their buffs before the statuses, and their hidden follow-up cooldowns after. */
  readonly own?: {
    readonly buffs: readonly FrameBuff[];
    readonly cooldowns: readonly HiddenCooldown[];
  };
  /** Rows under the HP bar. */
  readonly children?: ReactNode;
}

/** The side's mark: a dot for an ally, a bold cross for an enemy, drawn so the shape alone tells them apart in grey. */
export function SideMark({ side }: { readonly side: Side }) {
  return (
    <svg className="side-mark" viewBox="0 0 12 12" aria-hidden="true">
      {side === 'ally' ? (
        <circle cx="6" cy="6" r="4.5" />
      ) : (
        <path d="M2.5 2.5 9.5 9.5M9.5 2.5 2.5 9.5" />
      )}
    </svg>
  );
}

export function UnitFrame({
  side,
  name,
  level,
  hp,
  maxHp,
  shield,
  badge,
  label,
  hpName,
  size = 'full',
  dimmed,
  statuses,
  own,
  children,
}: UnitFrameProps) {
  const classes = [
    'unit-frame',
    `side-${side}`,
    size === 'compact' && 'unit-frame-compact',
    dimmed && 'unit-frame-dimmed',
  ];
  return (
    <div role="group" className={classes.filter(Boolean).join(' ')} aria-label={label}>
      <div className="unit-frame-head">
        <SideMark side={side} />
        <strong className="unit-frame-name" title={name}>
          {name}
        </strong>
        {badge && <span className="unit-frame-badge">{badge}</span>}
        <span className="unit-frame-level">Lv. {level}</span>
      </div>
      <Bar
        label="HP"
        value={hp}
        max={maxHp}
        kind="hp"
        {...(hpName && { name: hpName })}
        {...(shield !== undefined && { shield })}
      />
      {statuses && <FrameIcons statuses={statuses} {...(own && { own })} />}
      {children}
    </div>
  );
}
