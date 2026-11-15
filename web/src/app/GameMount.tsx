// Attached on mount and detached on unmount, which also covers React's development double mount.
import { useEffect } from 'react';

import type { GameView } from '@/game/view';

export interface GameMountProps {
  readonly view: GameView;
}

/** The game draws into #game, which index.html holds. */
export function GameMount({ view }: GameMountProps) {
  useEffect(() => {
    const parent = document.getElementById('game');
    if (!parent) {
      return undefined;
    }
    return view.attach(parent);
  }, [view]);
  return null;
}
