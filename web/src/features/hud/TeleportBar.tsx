// The teleport channel's bar (teleport.md section 4): "Teleporting to <map>" at the lower middle of the screen, filling over the channel while it runs.
import { limits } from '@/gamedata/limits';
import { mapName } from '@/progression/names';
import { teleport } from '@/state/teleport';

export function TeleportBar() {
  const to = teleport.use((s) => s.to);
  const started = teleport.use((s) => s.started);
  if (to === null) {
    return null;
  }
  return (
    <div className="teleport-bar" role="status">
      <span>Teleporting to {mapName(to)}</span>
      <div className="teleport-bar-track">
        <div
          key={started}
          className="teleport-bar-fill"
          style={{ animationDuration: `${limits.teleport_channel_ms}ms` }}
        />
      </div>
    </div>
  );
}
