// The compact party list on the HUD (C13), always visible while in a
// party: name, level, HP and whether they are online, from PartyState.
import { Bar } from '@/components/Bar';
import { party } from '@/state/party';

export function PartyPanel() {
  const current = party.use((s) => s.party);
  if (!current) {
    return null;
  }
  return (
    <div className="party-panel" aria-label="Party">
      {current.members.map((m) => (
        <div key={m.character_id} className={`party-panel-row${m.online ? '' : ' party-offline'}`}>
          <span>
            {m.character_id === current.leader ? '★ ' : ''}
            {m.name} <small>Lv. {m.level}</small>
          </span>
          <Bar label={`${m.name} HP`} value={m.hp} max={m.max_hp} kind="hp" />
        </div>
      ))}
    </div>
  );
}
