// Cached in localStorage so the first frame uses it before GET /api/settings answers.
import { defineStore } from './store';

export type Preset = 'classic' | 'wasd';

export const presets: readonly Preset[] = ['classic', 'wasd'];

export const PRESET_KEY = 'finga.settings.preset';

export interface SettingsState {
  readonly preset: Preset;
  setPreset(preset: Preset): void;
}

export function isPreset(value: unknown): value is Preset {
  return typeof value === 'string' && (presets as readonly string[]).includes(value);
}

/** Classic when the cache is missing or bad. */
export function loadCachedPreset(storage: Pick<Storage, 'getItem'> | null = safeStorage()): Preset {
  try {
    const value = storage?.getItem(PRESET_KEY);
    return isPreset(value) ? value : 'classic';
  } catch {
    return 'classic';
  }
}

export function cachePreset(
  preset: Preset,
  storage: Pick<Storage, 'setItem'> | null = safeStorage(),
): void {
  try {
    storage?.setItem(PRESET_KEY, preset);
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
  setPreset: (preset) => {
    cachePreset(preset);
    set({ preset });
  },
}));
