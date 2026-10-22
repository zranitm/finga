// The HUD (C09, C10): HP, MP and EXP bars and the level, from the
// character store. The EXP bar's maximum is the generated curve's value
// for the level, since no message carries it; at the cap the bar is full
// and says so. Each ExpGained shows for a moment as a gain or, for the
// death penalty, a loss.
import { useEffect, useState } from 'react';

import { Bar } from '@/components/Bar';
import { character } from '@/state/character';

const EXP_FLASH_MS = 2000;

export function Hud() {
  const c = character.use((s) => s.character);
  const expToNext = character.use((s) => s.expToNext);
  if (!c) {
    return null;
  }
  const capped = expToNext === null;
  return (
    <div className="hud" aria-label="Status">
      <div className="hud-title">
        <strong>{c.name}</strong>
        <span>Lv. {c.level}</span>
      </div>
      <Bar label="HP" value={c.hp} max={c.max_hp} kind="hp" />
      <Bar label="MP" value={c.mp} max={c.max_mp} kind="mp" />
      <Bar
        label="EXP"
        value={capped ? 1 : c.exp}
        max={capped ? 1 : expToNext}
        kind="exp"
        {...(capped && { text: 'MAX' })}
      />
      <ExpDelta />
    </div>
  );
}

/** The last EXP change, shown for two seconds: "+5 EXP" or, on the death penalty, "−1 EXP". */
function ExpDelta() {
  const change = character.use((s) => s.lastExpChange);
  const [hiddenSeq, setHiddenSeq] = useState(0);
  useEffect(() => {
    if (!change) {
      return undefined;
    }
    const id = setTimeout(() => setHiddenSeq(change.seq), EXP_FLASH_MS);
    return () => clearTimeout(id);
  }, [change]);
  if (!change || change.seq === hiddenSeq) {
    return null;
  }
  const loss = change.amount < 0;
  return (
    <span className={`exp-delta ${loss ? 'exp-loss' : 'exp-gain'}`} role="status">
      {loss ? `−${-change.amount}` : `+${change.amount}`} EXP
    </span>
  );
}
