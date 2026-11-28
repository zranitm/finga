// The teleport channel's bar (teleport.md section 4): "Teleporting to <waypoint>" (large-regions.md section 8), or "Switching to Channel <n>" (section 4), at the lower middle of the screen, filling over the channel while it runs.
import { limits } from '@/gamedata/limits';
import { teleport } from '@/state/teleport';

export function TeleportBar() {
  const to = teleport.use((s) => s.to);
  const started = teleport.use((s) => s.started);
  if (to === null) {
    return null;
  }
  const what = 'channel' in to ? `Switching to Channel ${to.channel}` : `Teleporting to ${to.name}`;
  return (
    <div className="teleport-bar" role="status">
      <span>{what}</span>
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
