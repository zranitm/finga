import { Bar } from '@/components/Bar';
import { mobName } from '@/progression/names';
import { fieldBoss } from '@/state/field-boss';

import { SideMark } from './UnitFrame';

/** The map's field boss in the right column while it is up and drawn, as a compact frame: the mark and its name on a row, its HP under it. */
export function BossBar() {
  const boss = fieldBoss.use((s) => s.boss);
  const hp = fieldBoss.use((s) => s.hp);
  if (!boss || !hp) {
    return null;
  }
  const name = mobName(boss.type);
  return (
    <div
      role="group"
      className="unit-frame unit-frame-compact boss-bar side-enemy"
      aria-label="Field boss"
    >
      <div className="unit-frame-head">
        <SideMark side="enemy" />
        <strong className="unit-frame-name" title={name}>
          {name}
        </strong>
      </div>
      <Bar label="HP" name={name} value={hp.hp} max={hp.maxHp} kind="hp" />
    </div>
  );
}
