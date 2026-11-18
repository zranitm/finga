// The account's settings, read after login; until they arrive, every flag off, the minimap on, and the keys this browser last played on.
import { readStored, writeStored } from '@/lib/storage';

import { defineStore } from './store';

const KEYS_KEY = 'finga.settings.keys';

export interface SettingsState {
  /** Spend each level-up's points by the class's split; the account's setting, off until it says so. */
  readonly autoAssignPoints: boolean;
  /** The chat stays open while the player isn't typing; the account's setting, off until it says so. */
  readonly chatPinned: boolean;
  /** The minimap in the corner; the account's setting, on until it says otherwise. */
  readonly showMinimap: boolean;
  /** The player's changes to the default keys, an action to a KeyboardEvent.code (key-bindings.md section 5). */
  readonly keys: Readonly<Record<string, string>>;
  setAutoAssignPoints(on: boolean): void;
  setChatPinned(on: boolean): void;
  setShowMinimap(on: boolean): void;
  setKeys(keys: Readonly<Record<string, string>>): void;
}

/** The keys this browser last played on, so the first frame uses them before GET /api/settings answers; none when the cache is missing or bad. */
function cachedKeys(): Readonly<Record<string, string>> {
  try {
    const value: unknown = JSON.parse(readStored(KEYS_KEY) ?? '{}');
    return typeof value === 'object' &&
      value !== null &&
      !Array.isArray(value) &&
      Object.values(value).every((code) => typeof code === 'string')
      ? (value as Record<string, string>)
      : {};
  } catch {
    return {};
  }
}

export const settings = defineStore<SettingsState>((set) => ({
  autoAssignPoints: false,
  chatPinned: false,
  showMinimap: true,
  keys: cachedKeys(),
  setAutoAssignPoints: (autoAssignPoints) => {
    set({ autoAssignPoints });
  },
  setChatPinned: (chatPinned) => {
    set({ chatPinned });
  },
  setShowMinimap: (showMinimap) => {
    set({ showMinimap });
  },
  setKeys: (keys) => {
    writeStored(KEYS_KEY, JSON.stringify(keys)); // a full or blocked storage loses only the fast start
    set({ keys });
  },
}));
