// The account's settings: read after login, and saved whole on each change, from whichever screen makes it.
import { saveQueue } from '@/lib/save-queue';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { settings } from '@/state/settings';

/** The account's saved settings win over the local ones once they arrive; until then, every flag off, the minimap on, the camera close and this browser's last keys stay in force. */
export async function loadSettings(rest: Pick<RestClient, 'getSettings'>): Promise<void> {
  try {
    const saved = await rest.getSettings();
    const s = settings.getState();
    s.setAutoAssignPoints(saved.auto_assign_points ?? false);
    s.setChatPinned(saved.chat_pinned ?? false);
    s.setShowMinimap(!(saved.hide_minimap ?? false));
    s.setZoom(saved.zoom ?? 0);
    s.setKeys(saved.keys ?? {});
  } catch {
    // Kept as they are until the next try.
  }
}

const ZOOM_SAVE_MS = 800; // a turn of the wheel settles before its save

/** Saves the settings a moment after the zoom stops changing, so a turn of the wheel makes one PUT, until the returned stop, which saves a change still waiting and settles once every save has. */
export function watchZoom(
  rest: Pick<RestClient, 'putSettings'>,
  notice: (text: string) => void,
): () => Promise<void> {
  const queue = saveQueue(
    () => () =>
      saveSettings(rest).then((problem) => {
        if (problem) {
          notice(problem);
        }
      }),
    ZOOM_SAVE_MS,
  );
  window.addEventListener('pagehide', queue.flush);
  const off = settings.subscribe((now, before) => {
    if (now.zoom !== before.zoom) {
      queue.changed();
    }
  });
  return () => {
    off();
    window.removeEventListener('pagehide', queue.flush);
    queue.flush();
    return queue.settled();
  };
}

/** A PUT replaces every setting, so it sends them all as they now stand. A failure keeps the local choice and returns what to tell the player. */
export async function saveSettings(rest: Pick<RestClient, 'putSettings'>): Promise<string | null> {
  const s = settings.getState();
  try {
    await rest.putSettings({
      auto_assign_points: s.autoAssignPoints,
      chat_pinned: s.chatPinned,
      hide_minimap: !s.showMinimap,
      zoom: s.zoom,
      keys: s.keys,
    });
    return null;
  } catch (err) {
    return err instanceof RestError ? err.message : 'Could not save the setting.';
  }
}
