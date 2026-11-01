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
        <div key={m.characterId} className={`party-panel-row${m.online ? '' : ' party-offline'}`}>
          <span>
            {m.characterId === current.leader ? '★ ' : ''}
            {m.name} <small>Lv. {m.level}</small>
          </span>
          <Bar label={`${m.name} HP`} value={m.hp} max={m.maxHp} kind="hp" />
        </div>
      ))}
    </div>
  );
}
