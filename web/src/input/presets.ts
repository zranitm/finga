// KeyboardEvent.code values, so the layout does not depend on the keyboard's language.
import type { Action } from './actions';

export type Preset = 'classic' | 'wasd';

export type KeyTable = Readonly<Record<Action, readonly string[]>>;

/** Keys 1 to 6 above the letters, in both presets. */
const skillDigits = {
  skill1: ['Digit1'],
  skill2: ['Digit2'],
  skill3: ['Digit3'],
  skill4: ['Digit4'],
  skill5: ['Digit5'],
  skill6: ['Digit6'],
} as const;

export const presets: Readonly<Record<Preset, KeyTable>> = {
  classic: {
    left: ['ArrowLeft'],
    right: ['ArrowRight'],
    up: ['ArrowUp'],
    down: ['ArrowDown'],
    jump: ['Space'],
    attack: ['KeyX'],
    talk: ['KeyZ'],
    hpPotion: ['KeyA'],
    ...skillDigits,
    inventory: ['KeyI'],
    character: ['KeyC'],
    skills: ['KeyK'],
    party: ['KeyP'],
    quests: ['KeyQ'],
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
    talk: ['KeyE'],
    hpPotion: ['KeyR'],
    ...skillDigits,
    inventory: ['KeyI'],
    character: ['KeyC'],
    skills: ['KeyK'],
    party: ['KeyP'],
    quests: ['KeyQ'],
    chat: ['Enter', 'NumpadEnter'],
    close: ['Escape'],
  },
};
