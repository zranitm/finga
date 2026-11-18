// The keys each action takes (key-bindings.md sections 2 and 5), as KeyboardEvent.code values: the key's place, not its letter, so the layout doesn't depend on the keyboard's language.
import { actions } from './actions';
import type { Action } from './actions';

export type KeyTable = Readonly<Record<Action, readonly string[]>>;

/** The player's changes to the defaults, an action to a code, as the account keeps them. */
export type KeyChanges = Readonly<Record<string, string>>;

/** Chat and close keep their keys, so a player can always reach the chat and close a window. */
export const fixedActions: ReadonlySet<Action> = new Set<Action>(['chat', 'close']);

/** Keys no action takes: the fixed actions' Enters and Esc, the system's Meta and menu keys, and the browser's F5, F11 and F12. The server refuses the same. */
export const reservedKeys: ReadonlySet<string> = new Set([
  'Escape',
  'Enter',
  'NumpadEnter',
  'MetaLeft',
  'MetaRight',
  'ContextMenu',
  'F5',
  'F11',
  'F12',
]);

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
  zoomIn: ['Equal'],
  zoomOut: ['Minus'],
  chat: ['Enter', 'NumpadEnter'],
  close: ['Escape'],
};

/** The keys that aren't a letter, a digit or on the numpad, named as a US keyboard prints them, for a browser that can't say what the player's prints. */
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

/** A key's name as hints and labels show it: what the player's keyboard prints on it where the browser says, else its US name. */
export function keyName(code: string, printed?: ReadonlyMap<string, string>): string {
  const own = printed?.get(code)?.trim();
  if (own) {
    const upper = own.toUpperCase();
    return upper.length === own.length ? upper : own; // ß would turn SS
  }
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

/** The skill bar's names for keys too long for its 40 px keys; the window and its lines keep the full ones. */
const short: Readonly<Record<string, string>> = {
  ShiftLeft: 'LShift',
  ShiftRight: 'RShift',
  ControlLeft: 'LCtrl',
  ControlRight: 'RCtrl',
  AltLeft: 'LAlt',
  AltRight: 'RAlt',
  Backspace: 'Bksp',
  Delete: 'Del',
  Insert: 'Ins',
  PageUp: 'PgUp',
  PageDown: 'PgDn',
  CapsLock: 'Caps',
};

/** A key's name short enough for a skill bar key: LShift for Left Shift, N5 for Num 5. */
export function shortKeyName(code: string, printed?: ReadonlyMap<string, string>): string {
  return short[code] ?? keyName(code, printed).replace(/^Num /, 'N');
}

/** The name of the first key an action takes. */
export function keyOf(
  table: KeyTable,
  action: Action,
  printed?: ReadonlyMap<string, string>,
): string {
  const code = table[action][0];
  return code ? keyName(code, printed) : '';
}

const tables = new WeakMap<KeyChanges, KeyTable>();

/** The defaults with the player's changes laid over. A change to an unknown or fixed action, to a reserved key, or to a key another action keeps by default is ignored, and that action keeps its default, so no key ever does two things. */
export function keyTable(changes: KeyChanges): KeyTable {
  const known = tables.get(changes);
  if (known) {
    return known;
  }
  let kept = actions.filter((a) => {
    const code = changes[a];
    return code !== undefined && !fixedActions.has(a) && !reservedKeys.has(code);
  });
  for (;;) {
    const held = new Set(actions.filter((a) => !kept.includes(a)).flatMap((a) => defaults[a]));
    const clash = kept.find((a) => {
      const code = changes[a]!;
      const taken = held.has(code);
      held.add(code);
      return taken;
    });
    if (clash === undefined) {
      break;
    }
    kept = kept.filter((a) => a !== clash); // back on its default, which a later pass checks again
  }
  const table: Record<Action, readonly string[]> = { ...defaults };
  for (const a of kept) {
    table[a] = [changes[a]!];
  }
  tables.set(changes, table);
  return table;
}

/** Binding code to action: an action holding code takes action's old key instead. Returns the changes to keep, only those that differ from the defaults, and the action moved, if any. */
export function rebind(
  changes: KeyChanges,
  action: Action,
  code: string,
): { readonly changes: KeyChanges; readonly moved: Action | null } {
  const table = keyTable(changes);
  const moved = actions.find((a) => a !== action && table[a].includes(code)) ?? null;
  const next: Record<string, string> = {};
  for (const a of actions) {
    const now = a === action ? code : a === moved ? table[action][0]! : table[a][0]!;
    if (!fixedActions.has(a) && now !== defaults[a][0]) {
      next[a] = now;
    }
  }
  return { changes: next, moved };
}
