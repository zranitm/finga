import type { Input } from '@/sim/input';

import type { KeyTable } from './bindings';

export type Action =
  | 'left'
  | 'right'
  | 'up' // walks up the screen, as down, left and right walk their ways
  | 'down'
  | 'jump'
  | 'walk' // switches between walk mode, which never sprints, and sprint mode
  | 'attack'
  | 'nextTarget' // locks the next mob or, in a match, enemy farther than the locked one
  | 'talk' // enters the portal the player stands in, else talks to the nearest NPC within reach
  | 'loot' // opens the loot window on the player's pouches within reach; open, takes them all
  | 'hpPotion' // the potion that best fills the HP missing
  | SkillKey // the skill on the skill bar's key
  | 'inventory'
  | 'character'
  | 'skills'
  | 'party'
  | 'quests'
  | 'map'
  | 'arena'
  | 'wardrobe'
  | 'zoomIn' // pulls the camera closer, as the wheel does
  | 'zoomOut'
  | 'chat'
  | 'close'; // the top-most window

export type SkillKey =
  'skill1' | 'skill2' | 'skill3' | 'skill4' | 'skill5' | 'skill6' | 'skill7' | 'skill8';

/** The skill bar's keys, in its order. */
export const skillKeys: readonly SkillKey[] = [
  'skill1',
  'skill2',
  'skill3',
  'skill4',
  'skill5',
  'skill6',
  'skill7',
  'skill8',
];

export const actions: readonly Action[] = [
  'left',
  'right',
  'up',
  'down',
  'jump',
  'walk',
  'attack',
  'nextTarget',
  'talk',
  'loot',
  'hpPotion',
  ...skillKeys,
  'inventory',
  'character',
  'skills',
  'party',
  'quests',
  'map',
  'arena',
  'wardrobe',
  'zoomIn',
  'zoomOut',
  'chat',
  'close',
];

const movement: readonly Action[] = ['left', 'right', 'up', 'down', 'jump'];

/** The action a key takes in the table, if any. */
export function actionFor(code: string, table: KeyTable): Action | undefined {
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
