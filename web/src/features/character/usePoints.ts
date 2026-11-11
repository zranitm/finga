import { useStaged } from '@/features/shared/useStaged';
import type { GameSocket } from '@/net/socket';
import { addStats, autoAssign, hasSplit, noPoints, total } from '@/progression/stats';
import type { Primary, PrimaryKey } from '@/progression/stats';
import { ERR } from '@/protocol/errors';
import { character } from '@/state/character';

export interface Points {
  readonly unspent: number;
  /** Staged, not yet applied. */
  readonly staged: Primary;
  /** Unspent less staged. */
  readonly left: number;
  readonly spent: number;
  readonly freeResetLeft: boolean;
  /** A request is on its way. */
  readonly busy: boolean;
  readonly notice: string | null;
  readonly stage: (k: PrimaryKey) => void;
  readonly undo: () => void;
  readonly apply: () => void;
  readonly autoAssign: () => void;
  /** The class has a split to auto-assign by; a Beginner has none. */
  readonly canAutoAssign: boolean;
  readonly reset: () => void;
}

export function usePoints(socket: Pick<GameSocket, 'request'>): Points {
  const c = character.use((s) => s.character);
  const p = c?.sheet?.points;
  const unspent = p?.unspent ?? 0;
  const draft = useStaged(unspent, noPoints, ERR.POINTS_CHANGED);
  const left = unspent - total(draft.staged);

  const spend = (points: Primary) => {
    if (total(points) > 0) {
      draft.send(() => socket.request('spendPoints', { expectedUnspent: unspent, points }));
    }
  };

  return {
    unspent,
    staged: draft.staged,
    left,
    spent: total(p?.spent ?? noPoints),
    freeResetLeft: p?.freeResetLeft ?? false,
    busy: draft.busy,
    notice: draft.notice,
    stage: (k) => {
      if (left > 0) {
        draft.stage(addStats(draft.staged, { [k]: 1 }));
      }
    },
    undo: draft.undo,
    apply: () => spend(draft.staged),
    autoAssign: () => spend(autoAssign(c?.class ?? '', unspent)),
    canAutoAssign: hasSplit(c?.class ?? ''),
    reset: () => draft.send(() => socket.request('resetPoints', { expectedUnspent: unspent })),
  };
}
