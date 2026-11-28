// The account's settings: read after login, and saved whole on each change, from whichever screen makes it; a read waits for the saves before it, and a setting changed while it was out keeps its change.
import { keyOf, keyTable } from '@/input/bindings';
import { saveQueue } from '@/lib/save-queue';
import type { SaveQueue } from '@/lib/save-queue';
import { RestError } from '@/net/rest';
import type { RestClient } from '@/net/rest';
import { keyboardLayout } from '@/state/keyboard-layout';
import { settings } from '@/state/settings';
import type { SettingsState } from '@/state/settings';

/** The queues of settings saves on screen, and the saves under way, which a read lets land first. */
const queues = new Set<SaveQueue>();
const sending = new Set<Promise<unknown>>();
let reading: Promise<void> = Promise.resolve(); // the read under way, which an entry's notice waits for

/** Tells reads of a queue saving the settings, till the returned stop: they save its change waiting at once and wait for it. */
export function holdReadsFor(queue: SaveQueue): () => void {
  queues.add(queue);
  return () => queues.delete(queue);
}

/** The account's saved settings win over the local ones once they arrive; until then, every flag off, the minimap on, the camera close, sprint mode and this browser's last keys stay in force. A change still waiting is saved first, so the read has it; a setting changed while the read is out keeps its change, and the read sets the rest. */
export function loadSettings(rest: Pick<RestClient, 'getSettings'>): Promise<void> {
  reading = readSettings(rest);
  return reading;
}

async function readSettings(rest: Pick<RestClient, 'getSettings'>): Promise<void> {
  const before = settings.getState();
  for (const q of queues) {
    q.flush();
  }
  await Promise.all([...[...queues].map((q) => q.settled()), ...sending]);
  try {
    const saved = await rest.getSettings();
    const s = settings.getState();
    const read = (k: keyof SettingsState) => s[k] === before[k]; // one changed while it was out is newer, and saves itself
    if (read('autoAssignPoints')) s.setAutoAssignPoints(saved.auto_assign_points ?? false);
    if (read('chatPinned')) s.setChatPinned(saved.chat_pinned ?? false);
    if (read('showMinimap')) s.setShowMinimap(!(saved.hide_minimap ?? false));
    if (read('zoom')) s.setZoom(saved.zoom ?? 0);
    if (read('walk')) s.setWalk(saved.walk ?? false);
    if (read('keys')) s.setKeys(saved.keys ?? {});
  } catch {
    // Kept as they are until the next try.
  }
}

/** What walk mode means, and the key that switches it: the walk tile's hover text and the notice on entering. */
export function walkModeText(key: string): string {
  return `Walk mode: no sprint. ${key} to switch.`;
}

/** Tells a player who entered with walk mode saved, once the account's settings are read, until the returned stop; nothing with sprint saved. */
export function tellWalkMode(notice: (text: string) => void): () => void {
  let live = true;
  void reading.then(() => {
    const s = settings.getState();
    if (live && s.walk) {
      notice(walkModeText(keyOf(keyTable(s.keys), 'walk', keyboardLayout.getState().printed)));
    }
  });
  return () => {
    live = false;
  };
}

const PLAY_SAVE_MS = 800; // a turn of the wheel, or a run of switches, settles before its save

/** Saves the settings a moment after those changed in play, the zoom and walk mode, stop changing, so a turn of the wheel makes one PUT, until the returned stop, which saves a change still waiting. */
export function watchPlaySettings(
  rest: Pick<RestClient, 'putSettings'>,
  notice: (text: string) => void,
): () => void {
  const queue = saveQueue(
    () => () =>
      saveSettings(rest).then((problem) => {
        if (problem) {
          notice(problem);
        }
      }),
    PLAY_SAVE_MS,
  );
  window.addEventListener('pagehide', queue.flush);
  const held = holdReadsFor(queue);
  const off = settings.subscribe((now, before) => {
    if (now.zoom !== before.zoom || now.walk !== before.walk) {
      queue.changed();
    }
  });
  return () => {
    off();
    held();
    window.removeEventListener('pagehide', queue.flush);
    queue.flush();
  };
}

/** A PUT replaces every setting, so it sends them all as they now stand. A failure keeps the local choice and returns what to tell the player. */
export async function saveSettings(rest: Pick<RestClient, 'putSettings'>): Promise<string | null> {
  const s = settings.getState();
  const put = rest.putSettings({
    auto_assign_points: s.autoAssignPoints,
    chat_pinned: s.chatPinned,
    hide_minimap: !s.showMinimap,
    zoom: s.zoom,
    walk: s.walk,
    keys: s.keys,
  });
  const landed = put.then(
    () => undefined,
    () => undefined,
  );
  sending.add(landed);
  void landed.then(() => sending.delete(landed));
  try {
    await put;
    return null;
  } catch (err) {
    return err instanceof RestError ? err.message : 'Could not save the setting.';
  }
}
