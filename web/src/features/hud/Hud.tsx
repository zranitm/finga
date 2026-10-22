// The HUD's first cut (C09): HP, MP and EXP bars and the level, from the
// character store. The EXP bar's maximum is the generated curve's value
// for the level, since no message carries it; at the cap the bar is full
// and says so.
import { Bar } from '@/components/Bar';
import { character } from '@/state/character';

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
    </div>
  );
}
