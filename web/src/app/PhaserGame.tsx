// Mounts the Phaser canvas behind the React screens (page 7, Phaser and
// React together): the game is created once on mount and destroyed on
// unmount, which also covers React's development double mount.
import { useEffect } from 'react';

import type { GameHandle } from '@/game/main';

export interface PhaserGameProps {
  readonly handle: GameHandle;
  /** The element the canvas goes in; index.html has #game. */
  readonly parentId?: string;
}

export function PhaserGame({ handle, parentId = 'game' }: PhaserGameProps) {
  useEffect(() => {
    const parent = document.getElementById(parentId);
    if (!parent) {
      return undefined;
    }
    return handle.attach(parent);
  }, [handle, parentId]);
  return null;
}
