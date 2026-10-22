// While a text field has focus, the game must ignore movement keys (page
// 7). Spread the returned handlers on any input so typing never leaks to
// the game, in a form before the world or in a window over it.
import type { FocusEventHandler } from 'react';

import { ui } from '@/state/ui';

export interface CaptureHandlers {
  readonly onFocus: FocusEventHandler<HTMLElement>;
  readonly onBlur: FocusEventHandler<HTMLElement>;
}

export function useKeyboardCapture(): CaptureHandlers {
  return {
    onFocus: () => {
      ui.getState().setCaptureKeyboard(true);
    },
    onBlur: () => {
      ui.getState().setCaptureKeyboard(false);
    },
  };
}
