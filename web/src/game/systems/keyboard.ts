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
  readonly preset: () => Preset;
  /** A text field or window has the keyboard. */
  readonly captured: () => boolean;
  /** Non-movement actions: attack, pickup, windows, chat. */
  readonly onAction?: (action: Action) => void;
  readonly source?: EventTarget;
  readonly focusSource?: EventTarget;
}

export function bindKeyboard(opts: KeyboardOptions): () => void {
  const source = opts.source ?? window;
  const focusSource = opts.focusSource ?? window;
  const movementFor = (e: Event): Action | undefined => {
    const code = (e as KeyboardEvent).code;
    const action = actionFor(code, opts.preset());
    return action !== undefined && isMovement(action) ? action : undefined;
  };
  const down = (e: Event) => {
    const key = e as KeyboardEvent;
    const action = actionFor(key.code, opts.preset());
    if (action === undefined) {
      return;
    }
    if (opts.captured()) {
      return;
    }
    if (!isMovement(action)) {
      if (!key.repeat) {
        opts.onAction?.(action);
      }
      return;
    }
    e.preventDefault(); // arrows and space would scroll the page
    if (!key.repeat) {
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
