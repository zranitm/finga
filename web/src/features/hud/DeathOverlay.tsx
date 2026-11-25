// The server respawns the player three seconds later; the overlay goes on Respawned. In a match a player who falls is down instead, till the round's reset gets them up, or in a battle till they get up in their base after its respawn (pvp.md section 13).
import { useState } from 'react';

import { isBattle } from '@/features/pvp/modes';
import { useNow } from '@/features/shared/useNow';
import { pvp as pvpData } from '@/gamedata/pvp';
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';

export function DeathOverlay() {
  const dead = character.use((s) => s.dead);
  const mode = pvp.use((s) => s.match?.mode ?? null);
  if (!dead) {
    return null;
  }
  if (mode !== null) {
    return (
      <div className="death-overlay down-overlay" role="alert">
        {isBattle(mode) ? <BackIn /> : <strong>Down until the round ends</strong>}
      </div>
    );
  }
  return (
    <div className="death-overlay" role="alert">
      <strong>You died.</strong>
      <span>Respawning…</span>
    </div>
  );
}

/** A battle's down, counted from when it showed, as the Died it shows for came, to the respawn's end. */
function BackIn() {
  const [since] = useState(() => performance.now());
  const left = Math.ceil((since + pvpData.battle.respawn_ms - useNow(250)) / 1000);
  return <strong>{left > 0 ? `Back in ${left} s` : 'Back in a moment'}</strong>;
}
