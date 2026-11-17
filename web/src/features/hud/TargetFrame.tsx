// The target window (target-lock.md sections 5 and 13): the locked mob's name, level, Elite tag and HP in an enemy's frame; the field boss has its own bar.
import { mobById } from '@/progression/mobs';
import { fieldBoss } from '@/state/field-boss';
import { target } from '@/state/target';

import { BossBar } from './BossBar';
import { UnitFrame } from './UnitFrame';

/** The locked mob, while there is one and it isn't the field boss: its HP as drawn over its kind's. */
export function TargetFrame() {
  const t = target.use((s) => s.target);
  const boss = fieldBoss.use((s) => s.boss?.mobId);
  const m = t && mobById(t.type);
  if (!t || !m || t.mobId === boss) {
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
      {...(m.elite && { badge: 'Elite' })}
    />
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
