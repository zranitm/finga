// A unit's statuses as icons with their time left (statuses.md section 6): hard control first, up to six, then "+N" for the rest.
import type { CSSProperties } from 'react';

import { Sweep } from '@/components/SkillIcon';
import { statusColor, statusIcon, statusName, statusOf } from '@/progression/statuses';
import type { ShownStatus } from '@/state/statuses';

const MOST = 6;
const GROUPS = ['hard', 'soft', 'dot', 'weaken', 'aggro'];

const rank = (id: string) => {
  const k = GROUPS.indexOf(statusOf(id)?.group ?? '');
  return k < 0 ? GROUPS.length : k;
};

const noop = () => undefined;

/** "Armor break": the status's name as a label. */
const titleOf = (id: string) => {
  const name = statusName(id);
  return name.charAt(0).toUpperCase() + name.slice(1);
};

/** The statuses in the order they show: by group, hard control first, then the soonest to end. */
function ordered(list: readonly ShownStatus[]): ShownStatus[] {
  return [...list].sort((a, b) => rank(a.id) - rank(b.id) || a.until - b.until);
}

/** The list's frame says whose they are. */
export function StatusIcons({ statuses }: { readonly statuses: readonly ShownStatus[] }) {
  if (statuses.length === 0) {
    return null;
  }
  const all = ordered(statuses);
  const rest = all.length - MOST;
  return (
    <ul className="status-icons" aria-label="Statuses">
      {all.slice(0, MOST).map((s) => (
        <li key={s.id} title={titleOf(s.id)} aria-label={titleOf(s.id)}>
          <StatusTile status={s} />
        </li>
      ))}
      {rest > 0 && (
        <li className="status-icons-more" aria-label={`${rest} more`}>
          +{rest}
        </li>
      )}
    </ul>
  );
}

/** A status's tile: its glyph on its colour, its time left swept and counted down. */
function StatusTile({ status }: { readonly status: ShownStatus }) {
  const icon = statusIcon(status.id);
  const style = {
    '--skill': `#${statusColor(status.id).toString(16).padStart(6, '0')}`,
    ...(icon && { '--icon': `url("${import.meta.env.BASE_URL}icons/statuses/${icon}.svg")` }),
  } as CSSProperties;
  return (
    <span className="skill-icon skill-icon-status" style={style} aria-hidden="true">
      <span className="skill-icon-face">{icon && <span className="skill-icon-glyph" />}</span>
      <Sweep key={`${status.from}:${status.until}`} cooldown={status} onEnd={noop} />
    </span>
  );
}
