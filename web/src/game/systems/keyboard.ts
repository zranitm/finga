import { actionFor, isHeld } from '@/input/actions';
import type { Action } from '@/input/actions';
import type { KeyTable } from '@/input/bindings';
import { anyModal } from '@/lib/modals';

export interface KeyTarget {
  /** again: the key was already down, held again by its repeat after a release-all let it go. */
  press(action: Action, again: boolean): void;
  release(action: Action): void;
  releaseAll(): void;
}

export interface KeyboardOptions {
  readonly target: KeyTarget;
  /** The keys each action takes now. */
  readonly keys: () => KeyTable;
  /** Whether the element an event went to is a text field, which has the keyboard. */
  readonly typing: (target: EventTarget | null) => boolean;
  /** One-shot actions: windows and chat; held ones (movement, attack) go to target. */
  readonly onAction?: (action: Action) => void;
}

const CTRL = ['ControlLeft', 'ControlRight'];
const ALT = ['AltLeft', 'AltRight'];

/** A key pressed with Ctrl, Cmd or Alt is a browser shortcut, such as Cmd+P, not a game key; but a Ctrl or Alt the player bound is a game key, its own press too, which comes with its flag set, and so is a key pressed while only bound ones are held. */
function shortcut(
  e: KeyboardEvent,
  held: ReadonlySet<string>,
  bound: (code: string) => boolean,
): boolean {
  const unbound = (codes: readonly string[]) => {
    const down = codes.filter((c) => c === e.code || held.has(c));
    return down.length === 0 || down.some((c) => !bound(c)); // none seen down: pressed before the page had the keys
  };
  return e.metaKey || (e.ctrlKey && unbound(CTRL)) || (e.altKey && unbound(ALT));
}

export function bindKeyboard(opts: KeyboardOptions): () => void {
  const pressed = new Set<string>(); // held keys pressed and not let go of since, by code
  const mods = new Set<string>(); // the Ctrl and Alt keys down, by code, bound or not
  const bound = (code: string) => actionFor(code, opts.keys()) !== undefined;
  const releaseAll = () => {
    pressed.clear();
    mods.clear();
    opts.target.releaseAll();
  };
  const heldFor = (e: KeyboardEvent): Action | undefined => {
    const action = actionFor(e.code, opts.keys());
    return action !== undefined && isHeld(action) ? action : undefined;
  };
  const down = (e: KeyboardEvent) => {
    const action = actionFor(e.code, opts.keys());
    const blocked = action === undefined || opts.typing(e.target) || shortcut(e, mods, bound);
    if (CTRL.includes(e.code) || ALT.includes(e.code)) {
      mods.add(e.code);
    }
    if (blocked || anyModal()) {
      return; // a modal is the only thing that takes keys while it's open
    }
    e.preventDefault(); // the game's keys: arrows and Space would scroll, and Space or Enter would press a focused button
    if (e.repeat && (pressed.has(e.code) || !isHeld(action))) {
      return; // a repeat presses only a key a release-all let go of while it was held
    }
    if (isHeld(action)) {
      pressed.add(e.code);
      opts.target.press(action, e.repeat);
    } else {
      opts.onAction?.(action);
    }
  };
  const up = (e: KeyboardEvent) => {
    if (e.key === 'Meta') {
      releaseAll(); // macOS sends no key up for a key let go while Cmd was held
      return;
    }
    if (bound(e.code) && !opts.typing(e.target) && !shortcut(e, mods, bound) && !anyModal()) {
      e.preventDefault(); // a focused button takes Space on its key up
    }
    mods.delete(e.code);
    const action = heldFor(e);
    if (action !== undefined) {
      pressed.delete(e.code);
      opts.target.release(action); // even while typing, so no key sticks
    }
  };
  const blur = () => {
    releaseAll();
  };
  const focusIn = (e: FocusEvent) => {
    if (opts.typing(e.target)) {
      releaseAll(); // keys held as typing starts would never see their key up
    }
  };
  window.addEventListener('keydown', down);
  window.addEventListener('keyup', up);
  window.addEventListener('blur', blur);
  window.addEventListener('focusin', focusIn);
  return () => {
    window.removeEventListener('keydown', down);
    window.removeEventListener('keyup', up);
    window.removeEventListener('blur', blur);
    window.removeEventListener('focusin', focusIn);
  };
}
