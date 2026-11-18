// The Keyboard Map API, in Chrome and Edge only and not in TypeScript's DOM types: what the player's layout prints on each key.
import { keyboardLayout } from '@/state/keyboard-layout';

interface LayoutKeyboard extends EventTarget {
  getLayoutMap(): Promise<Iterable<[string, string]>>;
}

/** Reads what the keyboard prints now, and again whenever the player switches layouts; a browser without the API keeps the US names. */
export function watchKeyboardLayout(): () => void {
  const keyboard = (navigator as Navigator & { readonly keyboard?: LayoutKeyboard }).keyboard;
  if (typeof keyboard?.getLayoutMap !== 'function') {
    return () => undefined;
  }
  let live = true;
  const read = () => {
    keyboard.getLayoutMap().then(
      (printed) => {
        if (live) {
          keyboardLayout.getState().setPrinted(new Map(printed));
        }
      },
      () => undefined, // refused, as in a cross-origin frame: the US names stay
    );
  };
  read();
  keyboard.addEventListener('layoutchange', read);
  return () => {
    live = false;
    keyboard.removeEventListener('layoutchange', read);
  };
}
