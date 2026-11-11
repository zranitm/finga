// The account's settings: read after login, and saved whole on each change, from whichever screen makes it.
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { settings } from '@/state/settings';

/** The account's saved settings win over the local ones once they arrive; until then, the cached preset and every flag off stay in force. */
export async function loadSettings(rest: Pick<RestClient, 'getSettings'>): Promise<void> {
  try {
    const saved = await rest.getSettings();
    const s = settings.getState();
    s.setPreset(saved.control_preset);
    s.setAutoAssignPoints(saved.auto_assign_points ?? false);
    s.setChatPinned(saved.chat_pinned ?? false);
  } catch {
    // Kept as they are until the next try.
  }
}

/** A PUT replaces every setting, so it sends them all as they now stand. A failure keeps the local choice and returns what to tell the player. */
export async function saveSettings(rest: Pick<RestClient, 'putSettings'>): Promise<string | null> {
  const s = settings.getState();
  try {
    await rest.putSettings({
      control_preset: s.preset,
      auto_assign_points: s.autoAssignPoints,
      chat_pinned: s.chatPinned,
    });
    return null;
  } catch (err) {
    return err instanceof RestError ? err.message : 'Could not save the setting.';
  }
}
