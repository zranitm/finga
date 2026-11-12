import { actionFor, isHeld } from '@/input/actions';
import type { Action } from '@/input/actions';
import type { Preset } from '@/state/settings';

export interface KeyTarget {
  /** again: the key was already down, held again by its repeat after a release-all let it go. */
  press(action: Action, again: boolean): void;
  release(action: Action): void;
  releaseAll(): void;
}

export interface KeyboardOptions {
  readonly target: KeyTarget;
  readonly preset: () => Preset;
  /** Whether the element an event went to is a text field, which has the keyboard. */
  readonly typing: (target: EventTarget | null) => boolean;
  /** One-shot actions: windows and chat; held ones (movement, attack) go to target. */
  readonly onAction?: (action: Action) => void;
}

/** A key pressed with Ctrl, Cmd or Alt is a browser shortcut, such as Cmd+P, not a game key. */
function shortcut(e: KeyboardEvent): boolean {
  return e.ctrlKey || e.metaKey || e.altKey;
}

export function bindKeyboard(opts: KeyboardOptions): () => void {
  const pressed = new Set<string>(); // held keys pressed and not let go of since, by code
  const releaseAll = () => {
    pressed.clear();
    opts.target.releaseAll();
  };
  const heldFor = (e: KeyboardEvent): Action | undefined => {
    const action = actionFor(e.code, opts.preset());
    return action !== undefined && isHeld(action) ? action : undefined;
  };
  const down = (e: KeyboardEvent) => {
    const action = actionFor(e.code, opts.preset());
    if (action === undefined || opts.typing(e.target) || shortcut(e)) {
      return;
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
    if (actionFor(e.code, opts.preset()) !== undefined && !opts.typing(e.target) && !shortcut(e)) {
      e.preventDefault(); // a focused button takes Space on its key up
    }
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
