// KeyboardEvent.code values, so the layout does not depend on the keyboard's language.
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
    character: ['KeyC'],
    party: ['KeyP'],
    chat: ['Enter', 'NumpadEnter'],
    close: ['Escape'],
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
    character: ['KeyC'],
    party: ['KeyP'],
    chat: ['Enter', 'NumpadEnter'],
    close: ['Escape'],
  },
};
