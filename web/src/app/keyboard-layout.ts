// The Keyboard Map API, in Chrome and Edge only and not in TypeScript's DOM types: what the player's layout prints on each key.
import { keyboardLayout } from '@/state/keyboard-layout';

/** Only getLayoutMap is assumed: Chrome's keyboard isn't an EventTarget, so it has no addEventListener. */
interface LayoutKeyboard {
  getLayoutMap(): Promise<Iterable<[string, string]>>;
  addEventListener?: unknown;
  removeEventListener?: unknown;
}

/** Reads what the keyboard prints now, and again when the window regains focus or, where the browser says, the layout changes; a browser without the API keeps the US names. */
export function watchKeyboardLayout(): () => void {
  const keyboard = (navigator as Navigator & { readonly keyboard?: LayoutKeyboard }).keyboard;
  if (typeof keyboard?.getLayoutMap !== 'function') {
    return () => undefined;
  }
  let live = true;
  const read = () => {
    try {
      keyboard.getLayoutMap().then(
        (printed) => {
          if (live) {
            keyboardLayout.getState().setPrinted(new Map(printed));
          }
        },
        () => undefined, // refused, as in a cross-origin frame: the US names stay
      );
    } catch {
      // a browser that throws rather than refuses keeps the US names too
    }
  };
  read();
  window.addEventListener('focus', read); // a layout switched in another window shows on return
  const target = keyboard as Partial<EventTarget>;
  const listens =
    typeof target.addEventListener === 'function' &&
    typeof target.removeEventListener === 'function';
  if (listens) {
    target.addEventListener!('layoutchange', read);
  }
  return () => {
    live = false;
    window.removeEventListener('focus', read);
    if (listens) {
      target.removeEventListener!('layoutchange', read);
    }
  };
}
