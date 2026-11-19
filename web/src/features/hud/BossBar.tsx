import { Bar } from '@/components/Bar';
import { mobName } from '@/progression/names';
import { fieldBoss } from '@/state/field-boss';

import { SideMark } from './UnitFrame';

/** The map's field boss at the top of the screen while it is up and drawn: its name and its HP. */
export function BossBar() {
  const boss = fieldBoss.use((s) => s.boss);
  const hp = fieldBoss.use((s) => s.hp);
  if (!boss || !hp) {
    return null;
  }
  return (
    <div role="group" className="boss-bar side-enemy" aria-label="Field boss">
      <SideMark side="enemy" />
      <Bar label={mobName(boss.type)} value={hp.hp} max={hp.maxHp} kind="hp" />
    </div>
  );
}
