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
  | 'inventory'
  | 'character'
  | 'party'
  | 'chat'
  | 'close'; // the top-most window

export const actions: readonly Action[] = [
  'left',
  'right',
  'up',
  'down',
  'jump',
  'attack',
  'inventory',
  'character',
  'party',
  'chat',
  'close',
];

const movement: readonly Action[] = ['left', 'right', 'up', 'down', 'jump'];

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

/** Acts while held: movement, and attack, which strikes again at each cooldown. */
export function isHeld(action: Action): boolean {
  return action === 'attack' || isMovement(action);
}

export function heldInput(held: ReadonlySet<Action>): Input {
  return {
    left: held.has('left'),
    right: held.has('right'),
    up: held.has('up'),
    down: held.has('down'),
    jump: held.has('jump'),
  };
}
