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
  /** A text field has the keyboard. */
  readonly typing: () => boolean;
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
    if (action === undefined || opts.typing()) {
      return;
    }
    e.preventDefault(); // the game's keys: arrows and Space would scroll, and Space or Enter would press a focused button
    if (key.repeat) {
      return;
    }
    if (isMovement(action)) {
      opts.target.press(action);
    } else {
      opts.onAction?.(action);
    }
  };
  const up = (e: Event) => {
    if (actionFor((e as KeyboardEvent).code, opts.preset()) !== undefined && !opts.typing()) {
      e.preventDefault(); // a focused button takes Space on its key up
    }
    const action = movementFor(e);
    if (action !== undefined) {
      opts.target.release(action); // even while typing, so no key sticks
    }
  };
  const blur = () => {
    opts.target.releaseAll();
  };
  const focusIn = () => {
    if (opts.typing()) {
      opts.target.releaseAll(); // keys held as typing starts would never see their key up
    }
  };
  source.addEventListener('keydown', down);
  source.addEventListener('keyup', up);
  focusSource.addEventListener('blur', blur);
  focusSource.addEventListener('focusin', focusIn);
  return () => {
    source.removeEventListener('keydown', down);
    source.removeEventListener('keyup', up);
    focusSource.removeEventListener('blur', blur);
    focusSource.removeEventListener('focusin', focusIn);
  };
}
