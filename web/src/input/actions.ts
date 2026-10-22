// Key codes become actions through the chosen preset (Screens and
// controls). Movement actions become the sim's Input; the others are
// commands and window toggles that the game and the features act on.
import type { Input } from '@/sim/input';

import { presets } from './presets';
import type { Preset } from './presets';

export type Action =
  | 'left'
  | 'right'
  | 'up' // climb, and enter a portal while standing on it
  | 'down'
  | 'jump' // with down on a one-way platform: drop through
  | 'attack'
  | 'pickup'
  | 'inventory'
  | 'party'
  | 'chat';

export const actions: readonly Action[] = [
  'left',
  'right',
  'up',
  'down',
  'jump',
  'attack',
  'pickup',
  'inventory',
  'party',
  'chat',
];

const movement: readonly Action[] = ['left', 'right', 'up', 'down', 'jump'];

/** The action a KeyboardEvent.code maps to under the preset, or undefined. */
export function actionFor(code: string, preset: Preset): Action | undefined {
  const table = presets[preset];
  for (const action of actions) {
    if (table[action].includes(code)) {
      return action;
    }
  }
  return undefined;
}

export function isMovement(action: Action): boolean {
  return movement.includes(action);
}

/** The sim Input for the actions currently held. */
export function heldInput(held: ReadonlySet<Action>): Input {
  return {
    left: held.has('left'),
    right: held.has('right'),
    up: held.has('up'),
    down: held.has('down'),
    jump: held.has('jump'),
  };
}
