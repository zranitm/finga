// The time left counts down here from what the server said when the boss appeared, or when the player joined.
import { useEffect, useState } from 'react';

import { Bar } from '@/components/Bar';
import { mobName } from '@/progression/names';
import { fieldBoss } from '@/state/field-boss';
import type { BossHp, FieldBoss } from '@/state/field-boss';

/** The map's field boss at the top of the screen while it is up and drawn: its name, its HP and the time it has left. */
export function BossBar() {
  const boss = fieldBoss.use((s) => s.boss);
  const hp = fieldBoss.use((s) => s.hp);
  return boss && hp ? <Shown boss={boss} hp={hp} /> : null;
}

function Shown({ boss, hp }: { readonly boss: FieldBoss; readonly hp: BossHp }) {
  const [at, setAt] = useState(() => performance.now());
  useEffect(() => {
    const id = setInterval(() => setAt(performance.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return (
    <div className="boss-bar" aria-label="Field boss">
      <Bar label={mobName(boss.type)} value={hp.hp} max={hp.maxHp} kind="hp" />
      <span className="boss-time" title="Time left">
        {timeLeft(boss.leavesAt - at)}
      </span>
    </div>
  );
}

/** m:ss, rounded up, and never below 0:00. */
export function timeLeft(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
