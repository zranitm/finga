// The match's fighters' frames (pvp.md section 13): in a round mode the teammates take the party frames' place and the enemies show in the right column under the field boss's place; each with its level, HP, statuses and a BOT tag, dimmed while down, and "Reconnecting" while their place is held through a drop.
import { UnitFrame } from '@/features/hud/UnitFrame';
import { character } from '@/state/character';
import { fighters } from '@/state/fighters';
import type { FighterLive } from '@/state/fighters';
import { pvp, sideOf } from '@/state/pvp';
import type { Fighter } from '@/state/pvp';

import { roundsToWin } from './modes';

/** The player's teammates in a round mode, in the party frames' place; none in 1v1. */
export function TeamFrames() {
  const match = pvp.use((s) => s.match);
  const me = character.use((s) => s.character?.id ?? '');
  const live = fighters.use((s) => s.live);
  if (!match || roundsToWin(match.mode) === 0) {
    return null;
  }
  const mates = (match.sides[sideOf(match, me) - 1] ?? []).filter((f) => f.id !== me && live[f.id]);
  return mates.length > 0 ? (
    <div role="group" className="party-panel" aria-label="Teammates">
      {mates.map((f) => (
        <Frame key={f.id} fighter={f} live={live[f.id]!} side="ally" />
      ))}
    </div>
  ) : null;
}

/** The enemies in a round mode, 1v1 included, one frame each in the right column; a battle's fifteen aren't framed. */
export function EnemyFrames() {
  const match = pvp.use((s) => s.match);
  const me = character.use((s) => s.character?.id ?? '');
  const live = fighters.use((s) => s.live);
  const side = sideOf(match, me);
  if (!match || side === 0 || roundsToWin(match.mode) === 0) {
    return null;
  }
  const foes = (match.sides[2 - side] ?? []).filter((f) => live[f.id]);
  return foes.length > 0 ? (
    <div role="group" className="enemy-frames" aria-label="Enemies">
      {foes.map((f) => (
        <Frame key={f.id} fighter={f} live={live[f.id]!} side="enemy" />
      ))}
    </div>
  ) : null;
}

function Frame({
  fighter: f,
  live,
  side,
}: {
  readonly fighter: Fighter;
  readonly live: FighterLive;
  readonly side: 'ally' | 'enemy';
}) {
  return (
    <UnitFrame
      side={side}
      name={f.name}
      level={live.level}
      hp={live.hp}
      maxHp={live.maxHp}
      label={`${side === 'ally' ? 'Teammate' : 'Enemy'} ${f.name}`}
      hpName={`${f.name} HP`}
      size="compact"
      dimmed={live.down}
      statuses={live.statuses}
      {...(f.bot && { badge: 'BOT' })}
    >
      {live.down && <p className="unit-frame-note">Down</p>}
      {f.away && <p className="unit-frame-note">Reconnecting</p>}
    </UnitFrame>
  );
}
