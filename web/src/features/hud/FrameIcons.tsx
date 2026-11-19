// A frame's icon row (frame-icons.md section 2): every frame's statuses, and on the player's own walk mode's icon first, their buffs before the statuses and their hidden follow-up cooldowns after. Up to twelve of each kind on the player's own, six on the others, then "+N" for it; the row wraps.
import { useReducer } from 'react';
import type { CSSProperties } from 'react';

import { Sweep } from '@/components/SkillIcon';
import { hudIcons } from '@/features/shared/hudIcons';
import { useKeys } from '@/features/shared/useKeys';
import { conditionText } from '@/progression/chains';
import { colorOf, pointsIn, skillById, stageAt } from '@/progression/skills';
import { statusColor, statusIcon, statusName, statusOf } from '@/progression/statuses';
import type { FrameBuff } from '@/state/character';
import { character } from '@/state/character';
import { party } from '@/state/party';
import type { ShownStatus } from '@/state/statuses';

import type { HiddenCooldown } from './hiddenCooldowns';

const MOST = 6;
const OWN_MOST = 12;
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

/** Whole seconds left, rounded up. */
const secondsTo = (until: number) => Math.max(0, Math.ceil((until - performance.now()) / 1000));

/** A skill's name at the stage points reach. */
const nameAt = (id: string, points: number) => {
  const s = skillById(id);
  return s?.stages[stageAt(s, points)]?.name ?? id;
};

/** The statuses in the order they show: by group, hard control first, then the soonest to end. */
function ordered(list: readonly ShownStatus[]): ShownStatus[] {
  return [...list].sort((a, b) => rank(a.id) - rank(b.id) || a.until - b.until);
}

/** What the player's own row shows besides statuses. */
export interface OwnIcons {
  readonly buffs: readonly FrameBuff[];
  readonly cooldowns: readonly HiddenCooldown[];
  /** Walk mode is on (large-regions.md section 7). */
  readonly walk: boolean;
}

export interface FrameIconsProps {
  readonly statuses: readonly ShownStatus[];
  /** The player's own frame's alone: given, the row is theirs, with room for twelve of each kind. */
  readonly own?: OwnIcons;
}

/** The list's frame says whose they are. */
export function FrameIcons({ statuses, own }: FrameIconsProps) {
  const buffs = own?.buffs ?? [];
  const cooldowns = own?.cooldowns ?? [];
  const walk = own?.walk === true;
  if (statuses.length + buffs.length + cooldowns.length === 0 && !walk) {
    return null;
  }
  const most = own ? OWN_MOST : MOST;
  return (
    <ul className="frame-icons" aria-label={own ? 'Buffs, statuses and cooldowns' : 'Statuses'}>
      {walk && <WalkTile />}
      {[...buffs]
        .sort((a, b) => a.until - b.until)
        .slice(0, most)
        .map((b) => (
          <BuffTile key={`${b.skill}:${b.giver}`} buff={b} />
        ))}
      <More n={buffs.length - most} kind="buffs" />
      {ordered(statuses)
        .slice(0, most)
        .map((s) => (
          <li key={s.id} title={titleOf(s.id)} aria-label={titleOf(s.id)}>
            <StatusTile status={s} />
          </li>
        ))}
      <More n={statuses.length - most} kind={own ? 'statuses' : ''} />
      {cooldowns.slice(0, most).map((c) => (
        <CooldownTile key={c.skill} cooling={c} />
      ))}
      <More n={cooldowns.length - most} kind="cooldowns" />
    </ul>
  );
}

/** "+N" for a kind's tiles past six; a frame with statuses alone says "N more". */
function More({ n, kind }: { readonly n: number; readonly kind: string }) {
  if (n <= 0) {
    return null;
  }
  return (
    <li className="frame-icons-more" aria-label={kind ? `${n} more ${kind}` : `${n} more`}>
      +{n}
    </li>
  );
}

const WALK_STYLE = {
  '--skill': '#5d7a96',
  '--icon': `url("${import.meta.env.BASE_URL}icons/hud/${hudIcons.walk}.svg")`,
} as CSSProperties;

/** Walk mode's tile, while it's on: a player who logs in walking sees why they never sprint, and the key that switches it. */
function WalkTile() {
  const keys = useKeys();
  return (
    <li title={`Walk mode: no sprint. ${keys.name('walk')} to switch.`} aria-label="Walk mode">
      <span
        className="skill-icon skill-icon-status frame-icon-walk"
        style={WALK_STYLE}
        aria-hidden="true"
      >
        <span className="skill-icon-face">
          <span className="skill-icon-glyph" />
        </span>
      </span>
    </li>
  );
}

/** A tooltip read as the pointer comes onto it, so its seconds are those left then. */
function useHoverNow(): () => void {
  const [, hovered] = useReducer((n: number) => n + 1, 0);
  return hovered;
}

/** A buff's tile: its skill's icon at its stage, in its colour, edged ally green, its time left swept and counted. */
function BuffTile({ buff }: { readonly buff: FrameBuff }) {
  const hovered = useHoverNow();
  const members = party.use((s) => s.party?.members);
  const s = skillById(buff.skill);
  const st = s?.stages[stageAt(s, buff.points)];
  const name = st?.name ?? buff.skill;
  const giver =
    buff.giver && (members?.find((m) => m.characterId === buff.giver)?.name ?? 'an ally');
  const tip = `${name}: ${secondsTo(buff.until)} s left${giver ? `, from ${giver}` : ''}`;
  const style =
    s && st
      ? ({
          '--skill': `#${colorOf(s, st).toString(16).padStart(6, '0')}`,
          '--icon': `url("${import.meta.env.BASE_URL}icons/skills/${st.icon}.svg")`,
        } as CSSProperties)
      : undefined;
  return (
    <li title={tip} aria-label={name} onMouseEnter={hovered}>
      <span
        className="skill-icon skill-icon-status frame-icon-buff"
        style={style}
        aria-hidden="true"
      >
        <span className="skill-icon-face">{st && <span className="skill-icon-glyph" />}</span>
        <Sweep key={`${buff.from}:${buff.until}`} cooldown={buff} onEnd={noop} />
      </span>
    </li>
  );
}

/** A hidden cooldown's tile: its skill's icon dimmed and edged grey, with the bar's dark sweep and the seconds to ready. */
function CooldownTile({ cooling }: { readonly cooling: HiddenCooldown }) {
  const hovered = useHoverNow();
  const invested = character.use((s) => s.character?.sheet?.skills?.invested) ?? {};
  const s = skillById(cooling.skill);
  const points = s ? pointsIn(s, invested) : 0;
  const st = s?.stages[stageAt(s, points)];
  const name = st?.name ?? cooling.skill;
  const keys = cooling.on.map(({ root, when }) => {
    const rootName = nameAt(root, invested[root] ?? 0);
    return `on ${rootName}’s key ${conditionText(when, rootName)}`;
  });
  const where = keys.join(', or ');
  const tip = `${name}: ready in ${secondsTo(cooling.cooldown.until)} s. ${where.charAt(0).toUpperCase()}${where.slice(1)}.`;
  const style =
    s && st
      ? ({
          '--skill': `#${colorOf(s, st).toString(16).padStart(6, '0')}`,
          '--icon': `url("${import.meta.env.BASE_URL}icons/skills/${st.icon}.svg")`,
        } as CSSProperties)
      : undefined;
  return (
    <li title={tip} aria-label={`${name}, cooling`} onMouseEnter={hovered}>
      <span
        className="skill-icon skill-icon-status frame-icon-cooling"
        style={style}
        aria-hidden="true"
      >
        <span className="skill-icon-face">{st && <span className="skill-icon-glyph" />}</span>
        <Sweep
          key={`${cooling.cooldown.from}:${cooling.cooldown.until}`}
          cooldown={cooling.cooldown}
          onEnd={noop}
        />
      </span>
    </li>
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
