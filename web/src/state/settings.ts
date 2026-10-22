// The control preset (B6). Saved to the account by the settings feature and
// cached in localStorage so the first frame uses it before GET /api/settings
// answers; a bad or missing cache falls back to Classic.
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

/** The cached preset, or Classic when there is none or it is not a preset. */
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
