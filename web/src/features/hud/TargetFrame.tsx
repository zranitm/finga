// The target window (target-lock.md section 5): the locked mob's name, level, Elite tag and HP; the field boss has its own bar.
import { Bar } from '@/components/Bar';
import { mobById } from '@/progression/mobs';
import { fieldBoss } from '@/state/field-boss';
import { target } from '@/state/target';

import { BossBar } from './BossBar';

/** The locked mob, while there is one and it isn't the field boss: its HP as drawn over its kind's. */
export function TargetFrame() {
  const t = target.use((s) => s.target);
  const boss = fieldBoss.use((s) => s.boss?.mobId);
  const m = t && mobById(t.type);
  if (!t || !m || t.mobId === boss) {
    return null;
  }
  return (
    <div className="target-frame" aria-label="Target">
      <span className="target-level">Lv. {m.level}</span>
      {m.elite && <span className="target-elite">Elite</span>}
      <Bar label={m.name} value={t.hp} max={m.hp} kind="hp" />
    </div>
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
