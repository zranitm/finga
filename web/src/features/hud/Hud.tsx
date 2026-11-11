// The EXP bar's maximum comes from the generated curve, since no message carries it.
import { useEffect, useState } from 'react';

import { Bar } from '@/components/Bar';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { ui } from '@/state/ui';

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
      <Bar label="HP" value={c.hp} max={c.maxHp} kind="hp" />
      <Bar label="MP" value={c.mp} max={c.maxMp} kind="mp" />
      <Bar
        label="EXP"
        value={capped ? 1 : Number(c.exp)}
        max={capped ? 1 : expToNext}
        kind="exp"
        {...(capped && { text: 'MAX' })}
      />
      <ExpDelta />
      <PointsBadge />
      <SkillPointsBadge />
      <WaitingNotice />
    </div>
  );
}

/** Unspent stat points, until spent; it opens the character window to spend them. */
function PointsBadge() {
  const unspent = character.use((s) => s.character?.sheet?.points?.unspent ?? 0);
  return unspent > 0 ? (
    <button
      type="button"
      className="hud-points"
      onClick={() => ui.getState().openWindow('character')}
    >
      {unspent} stat {unspent === 1 ? 'point' : 'points'}
    </button>
  ) : null;
}

/** Unspent skill points, until spent; it opens the skill window to spend them. */
function SkillPointsBadge() {
  const unspent = character.use((s) => s.character?.sheet?.skills?.unspent ?? 0);
  return unspent > 0 ? (
    <button type="button" className="hud-points" onClick={() => ui.getState().openWindow('skills')}>
      {unspent} skill {unspent === 1 ? 'point' : 'points'}
    </button>
  ) : null;
}

function WaitingNotice() {
  const waiting = combatLog.use((s) => s.waiting.length);
  return waiting > 0 ? (
    <span className="hud-waiting">Inventory full: {waiting} waiting</span>
  ) : null;
}

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
