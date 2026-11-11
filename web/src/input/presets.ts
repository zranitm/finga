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
    talk: ['KeyZ'],
    hpPotion: ['KeyA'],
    mpPotion: ['KeyS'],
    inventory: ['KeyI'],
    character: ['KeyC'],
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
    mpPotion: ['KeyF'],
    inventory: ['KeyI'],
    character: ['KeyC'],
    party: ['KeyP'],
    quests: ['KeyQ'],
    chat: ['Enter', 'NumpadEnter'],
    close: ['Escape'],
  },
};
