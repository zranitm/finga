// The target window (target-lock.md sections 5, 13 and 14): the locked mob's name, level, Elite tag, HP and stats in a compact enemy's frame, the field boss's too.
import { mobById } from '@/progression/mobs';
import type { MobType } from '@/progression/mobs';
import { damageColor } from '@/progression/skills';
import { statuses } from '@/state/statuses';
import { target } from '@/state/target';

import { BossBar } from './BossBar';
import { UnitFrame } from './UnitFrame';

/** The locked mob, while there is one: its HP as drawn over its kind's, and its kind's stats. */
export function TargetFrame() {
  const t = target.use((s) => s.target);
  const on = statuses.use((s) => s.target);
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
