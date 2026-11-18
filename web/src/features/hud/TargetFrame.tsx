// The target window (target-lock.md sections 5, 13 and 14, pvp.md section 13): the locked mob's name, level, Elite tag, HP and stats in a compact enemy's frame, the field boss's too; in a match a locked player's name, level, class, BOT tag and HP.
import { classes } from '@/gamedata/classes';
import { mobById } from '@/progression/mobs';
import type { MobType } from '@/progression/mobs';
import { damageColor } from '@/progression/skills';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';
import type { PlayerTarget } from '@/state/target';

import { BossBar } from './BossBar';
import { UnitFrame } from './UnitFrame';

/** The lock, while there is one: a mob's HP as drawn over its kind's, and its kind's stats; a player's own. */
export function TargetFrame() {
  const t = target.use((s) => s.target);
  const on = statuses.use((s) => s.target);
  if (t?.kind === 'player') {
    return <PlayerFrame player={t} />;
  }
  const m = t && mobById(t.type);
  if (!t || !m) {
    return null;
  }
  return (
    <UnitFrame
      side="enemy"
      name={m.name}
      level={m.level}
      hp={t.hp}
      maxHp={m.hp}
      label={`Target: ${m.name}, enemy`}
      hpName={`${m.name} HP`}
      size="compact"
      statuses={on}
      {...(m.elite && { badge: 'Elite' })}
    >
      <Stats mob={m} />
    </UnitFrame>
  );
}

/** A locked enemy player: their name, level, class and HP, and a BOT tag on a bot. */
function PlayerFrame({ player: p }: { readonly player: PlayerTarget }) {
  const on = statuses.use((s) => s.target);
  const cls = classes.find((c) => c.id === p.cls)?.name ?? p.cls;
  return (
    <UnitFrame
      side="enemy"
      name={p.name}
      level={p.level}
      hp={p.hp}
      maxHp={p.maxHp}
      label={`Target: ${p.name}, enemy`}
      hpName={`${p.name} HP`}
      size="compact"
      statuses={on}
      {...(p.bot && { badge: 'BOT' })}
    >
      <p className="unit-frame-class">{cls}</p>
    </UnitFrame>
  );
}

/** A mob's stats on one line, wrapping to two: each short, with the Character window's full name to hover and to a screen reader; ATK's name in its damage type's colour. */
function Stats({ mob }: { readonly mob: MobType }) {
  const stats = [
    {
      short: 'ATK',
      name: mob.damage === 'magic' ? 'Magic attack' : 'Physical attack',
      value: mob.attack,
      color: `#${damageColor(mob.damage).toString(16).padStart(6, '0')}`,
    },
    { short: 'DEF', name: 'Physical defense', value: mob.physical_defense },
    { short: 'M.DEF', name: 'Magic defense', value: mob.magic_defense },
    { short: 'ACC', name: 'Accuracy', value: mob.accuracy },
    { short: 'EVA', name: 'Evasion', value: mob.evasion },
  ];
  return (
    <ul className="unit-frame-stats" aria-label={`${mob.name} stats`}>
      {stats.map((s) => (
        <li key={s.short} title={s.name} aria-label={`${s.name} ${s.value}`}>
          <span className="unit-frame-stat" {...('color' in s && { style: { color: s.color } })}>
            {s.short}
          </span>
          {s.value}
        </li>
      ))}
    </ul>
  );
}

/** The bars at the top centre: the field boss's, then the target window under it. */
export function TopBars() {
  return (
    <div className="top-bars">
      <BossBar />
      <TargetFrame />
    </div>
  );
}
