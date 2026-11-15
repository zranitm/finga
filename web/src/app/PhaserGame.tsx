// Created on mount and destroyed on unmount, which also covers React's development double mount.
import { useEffect } from 'react';

import type { GameView } from '@/game/main';

export interface PhaserGameProps {
  readonly handle: GameView;
}

/** The game draws into #game, which index.html holds. */
export function PhaserGame({ handle }: PhaserGameProps) {
  useEffect(() => {
    const parent = document.getElementById('game');
    if (!parent) {
      return undefined;
    }
    return handle.attach(parent);
  }, [handle]);
  return null;
}
