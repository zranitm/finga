// The two presets from Screens and controls, as KeyboardEvent.code values
// so the layout does not depend on the keyboard's language. I, P and Enter
// are shared.
import type { Action } from './actions';

export type Preset = 'classic' | 'wasd';

export type KeyTable = Readonly<Record<Action, readonly string[]>>;

export const presets: Readonly<Record<Preset, KeyTable>> = {
  classic: {
    left: ['ArrowLeft'],
    right: ['ArrowRight'],
    up: ['ArrowUp'],
    down: ['ArrowDown'],
    jump: ['Space'],
    attack: ['KeyX'],
    pickup: ['KeyZ'],
    inventory: ['KeyI'],
    party: ['KeyP'],
    chat: ['Enter', 'NumpadEnter'],
  },
  wasd: {
    left: ['KeyA'],
    right: ['KeyD'],
    up: ['KeyW'],
    down: ['KeyS'],
    jump: ['Space'],
    attack: ['KeyJ'],
    pickup: ['KeyK'],
    inventory: ['KeyI'],
    party: ['KeyP'],
    chat: ['Enter', 'NumpadEnter'],
  },
};

/** A short label for the first key of an action, for hints and the settings screen. */
export function keyLabel(preset: Preset, action: Action): string {
  const code = presets[preset][action][0] ?? '';
  return code
    .replace(/^Key/, '')
    .replace(/^Arrow/, '')
    .replace(/^Numpad/, '');
}
