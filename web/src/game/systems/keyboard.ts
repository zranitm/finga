// Keyboard to movement actions: KeyboardEvent.code through the player's
// preset (input/presets) into the input sender. Only movement actions are
// bound here; attack, pickup and the windows come with their steps.
import { actionFor, isMovement } from '@/input/actions';
import type { Action } from '@/input/actions';
import type { Preset } from '@/state/settings';

export interface KeyTarget {
  press(action: Action): void;
  release(action: Action): void;
  releaseAll(): void;
}

export interface KeyboardOptions {
  readonly target: KeyTarget;
  /** The preset in force right now (the settings store). */
  readonly preset: () => Preset;
  /** Whether a text field or window has the keyboard (the ui store). */
  readonly captured: () => boolean;
  /** Where key events arrive; the window by default. */
  readonly source?: EventTarget;
  /** What loses focus when the player switches away; the window by default. */
  readonly focusSource?: EventTarget;
}

/** Binds the listeners; returns the function that unbinds them. */
export function bindKeyboard(opts: KeyboardOptions): () => void {
  const source = opts.source ?? window;
  const focusSource = opts.focusSource ?? window;
  const movementFor = (e: Event): Action | undefined => {
    const code = (e as KeyboardEvent).code;
    const action = actionFor(code, opts.preset());
    return action !== undefined && isMovement(action) ? action : undefined;
  };
  const down = (e: Event) => {
    const action = movementFor(e);
    if (action === undefined) {
      return;
    }
    if (opts.captured()) {
      return; // typing: the keys belong to the text field
    }
    e.preventDefault(); // arrows and space would scroll the page
    if (!(e as KeyboardEvent).repeat) {
      opts.target.press(action);
    }
  };
  const up = (e: Event) => {
    const action = movementFor(e);
    if (action !== undefined) {
      opts.target.release(action); // even while captured, so no key sticks
    }
  };
  const blur = () => {
    opts.target.releaseAll();
  };
  source.addEventListener('keydown', down);
  source.addEventListener('keyup', up);
  focusSource.addEventListener('blur', blur);
  return () => {
    source.removeEventListener('keydown', down);
    source.removeEventListener('keyup', up);
    focusSource.removeEventListener('blur', blur);
  };
}
