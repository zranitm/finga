// The server respawns the player three seconds later; the overlay goes on Respawned.
import { character } from '@/state/character';

export function DeathOverlay() {
  const dead = character.use((s) => s.dead);
  if (!dead) {
    return null;
  }
  return (
    <div className="death-overlay" role="alert">
      <strong>You died.</strong>
      <span>Respawning…</span>
    </div>
  );
}
