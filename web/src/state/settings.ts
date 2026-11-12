// Cached in localStorage so the first frame uses it before GET /api/settings answers.
import { defineStore } from './store';

export type Preset = 'classic' | 'wasd';

export const presets: readonly Preset[] = ['classic', 'wasd'];

export const PRESET_KEY = 'finga.settings.preset';

export interface SettingsState {
  readonly preset: Preset;
  /** Spend each level-up's points by the class's split; the account's setting, off until it says so. */
  readonly autoAssignPoints: boolean;
  /** The chat stays open while the player isn't typing; the account's setting, off until it says so. */
  readonly chatPinned: boolean;
  setPreset(preset: Preset): void;
  setAutoAssignPoints(on: boolean): void;
  setChatPinned(on: boolean): void;
}

export function isPreset(value: unknown): value is Preset {
  return typeof value === 'string' && (presets as readonly string[]).includes(value);
}

/** Classic when the cache is missing or bad. */
export function loadCachedPreset(): Preset {
  try {
    const value = safeStorage()?.getItem(PRESET_KEY);
    return isPreset(value) ? value : 'classic';
  } catch {
    return 'classic';
  }
}

export function cachePreset(preset: Preset): void {
  try {
    safeStorage()?.setItem(PRESET_KEY, preset);
  } catch {
    // A full or blocked storage only loses the fast start, not the setting.
  }
}

function safeStorage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

export const settings = defineStore<SettingsState>((set) => ({
  preset: loadCachedPreset(),
  autoAssignPoints: false,
  chatPinned: false,
  setPreset: (preset) => {
    cachePreset(preset);
    set({ preset });
  },
  setAutoAssignPoints: (autoAssignPoints) => {
    set({ autoAssignPoints });
  },
  setChatPinned: (chatPinned) => {
    set({ chatPinned });
  },
}));
