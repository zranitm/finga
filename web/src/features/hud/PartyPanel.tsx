import { party } from '@/state/party';
import { statuses } from '@/state/statuses';

import { UnitFrame } from './UnitFrame';

export function PartyPanel() {
  const current = party.use((s) => s.party);
  const on = statuses.use((s) => s.party);
  if (!current) {
    return null;
  }
  return (
    <div role="group" className="party-panel" aria-label="Party">
      {current.members.map((m) => (
        <UnitFrame
          key={m.characterId}
          side="ally"
          name={`${m.characterId === current.leader ? '★ ' : ''}${m.name}`}
          level={m.level}
          hp={m.hp}
          maxHp={m.maxHp}
          label={`Party member ${m.name}`}
          hpName={`${m.name} HP`}
          size="compact"
          dimmed={!m.online}
          {...(on[m.characterId] && { statuses: on[m.characterId] })}
        />
      ))}
    </div>
  );
}
