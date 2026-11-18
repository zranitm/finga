// The account's settings, read after login; until they arrive, every flag off and the minimap on.
import { defineStore } from './store';

export interface SettingsState {
  /** Spend each level-up's points by the class's split; the account's setting, off until it says so. */
  readonly autoAssignPoints: boolean;
  /** The chat stays open while the player isn't typing; the account's setting, off until it says so. */
  readonly chatPinned: boolean;
  /** The minimap in the corner; the account's setting, on until it says otherwise. */
  readonly showMinimap: boolean;
  setAutoAssignPoints(on: boolean): void;
  setChatPinned(on: boolean): void;
  setShowMinimap(on: boolean): void;
}

export const settings = defineStore<SettingsState>((set) => ({
  autoAssignPoints: false,
  chatPinned: false,
  showMinimap: true,
  setAutoAssignPoints: (autoAssignPoints) => {
    set({ autoAssignPoints });
  },
  setChatPinned: (chatPinned) => {
    set({ chatPinned });
  },
  setShowMinimap: (showMinimap) => {
    set({ showMinimap });
  },
}));
