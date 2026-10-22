// Shown while the local player is dead (C10): the server respawns them
// three seconds later, and the overlay goes when Respawned arrives.
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
