import { roundsToWin } from '@/features/pvp/modes';
import { isMatchMap } from '@/progression/names';
import { party } from '@/state/party';
import { pvp } from '@/state/pvp';
import { statuses } from '@/state/statuses';

import { UnitFrame } from './UnitFrame';

export function PartyPanel() {
  const current = party.use((s) => s.party);
  const on = statuses.use((s) => s.party);
  const rounds = pvp.use((s) => (s.match ? roundsToWin(s.match.mode) > 0 : false));
  if (!current || rounds) {
    return null; // a round mode's teammates take the party's place (pvp.md section 13)
  }
  return (
    <div role="group" className="party-panel" aria-label="Party">
      {current.members.map((m) => {
        const shown = on[m.characterId];
        return (
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
            {...(shown && { statuses: shown })}
          >
            {shown === null && <p className="unit-frame-note">Out of sight</p>}
            {isMatchMap(m.mapId) && <p className="unit-frame-note">In a match</p>}
          </UnitFrame>
        );
      })}
    </div>
  );
}
