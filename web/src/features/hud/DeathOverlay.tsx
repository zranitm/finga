// The server respawns the player three seconds later; the overlay goes on Respawned. In a match a player who falls is down instead, till the round's reset gets them up (pvp.md section 13).
import { character } from '@/state/character';
import { pvp } from '@/state/pvp';

export function DeathOverlay() {
  const dead = character.use((s) => s.dead);
  const inMatch = pvp.use((s) => s.match !== null);
  if (!dead) {
    return null;
  }
  if (inMatch) {
    return (
      <div className="death-overlay down-overlay" role="alert">
        <strong>Down until the round ends</strong>
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
