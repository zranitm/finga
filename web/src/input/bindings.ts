// The keys each action takes (key-bindings.md section 2), as KeyboardEvent.code values: the key's place, not its letter, so the layout doesn't depend on the keyboard's language.
import type { Action } from './actions';

export type KeyTable = Readonly<Record<Action, readonly string[]>>;

/** One key per action, the skills on the left hand's Q W E R A S D F; chat alone takes two, both Enters. */
export const defaults: KeyTable = {
  left: ['ArrowLeft'],
  right: ['ArrowRight'],
  up: ['ArrowUp'],
  down: ['ArrowDown'],
  jump: ['Space'],
  attack: ['KeyX'],
  nextTarget: ['Tab'],
  talk: ['KeyZ'],
  hpPotion: ['Digit1'],
  skill1: ['KeyQ'],
  skill2: ['KeyW'],
  skill3: ['KeyE'],
  skill4: ['KeyR'],
  skill5: ['KeyA'],
  skill6: ['KeyS'],
  skill7: ['KeyD'],
  skill8: ['KeyF'],
  inventory: ['KeyI'],
  character: ['KeyC'],
  skills: ['KeyK'],
  party: ['KeyP'],
  quests: ['KeyJ'],
  map: ['KeyM'],
  chat: ['Enter', 'NumpadEnter'],
  close: ['Escape'],
};

/** The keys that aren't a letter, a digit or on the numpad, named as a US keyboard prints them. */
const named: Readonly<Record<string, string>> = {
  Space: 'Space',
  Tab: 'Tab',
  Enter: 'Enter',
  NumpadEnter: 'Num Enter',
  Escape: 'Esc',
  ArrowUp: '↑',
  ArrowDown: '↓',
  ArrowLeft: '←',
  ArrowRight: '→',
  ShiftLeft: 'Left Shift',
  ShiftRight: 'Right Shift',
  ControlLeft: 'Left Ctrl',
  ControlRight: 'Right Ctrl',
  AltLeft: 'Left Alt',
  AltRight: 'Right Alt',
  Backquote: '`',
  Minus: '-',
  Equal: '=',
  BracketLeft: '[',
  BracketRight: ']',
  Backslash: '\\',
  Semicolon: ';',
  Quote: "'",
  Comma: ',',
  Period: '.',
  Slash: '/',
};

/** A key's name as hints and labels show it. */
export function keyName(code: string): string {
  const letter = /^Key([A-Z])$/.exec(code) ?? /^Digit([0-9])$/.exec(code);
  if (letter) {
    return letter[1]!;
  }
  const pad = /^Numpad(.+)$/.exec(code);
  if (pad) {
    return `Num ${pad[1]}`;
  }
  return named[code] ?? code;
}

/** The name of the first key an action takes. */
export function keyOf(table: KeyTable, action: Action): string {
  const code = table[action][0];
  return code ? keyName(code) : '';
}
