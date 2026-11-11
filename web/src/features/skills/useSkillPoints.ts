import { useStaged } from '@/features/shared/useStaged';
import type { GameSocket } from '@/net/socket';
import { ERR } from '@/protocol/errors';
import { character } from '@/state/character';

export type SkillPointMap = Readonly<Record<string, number>>;

export interface SkillPoints {
  readonly unspent: number;
  /** The points in each skill, from the server's sheet. */
  readonly invested: SkillPointMap;
  /** Staged, not yet applied. */
  readonly staged: SkillPointMap;
  /** Unspent less staged. */
  readonly left: number;
  readonly busy: boolean;
  readonly notice: string | null;
  readonly stage: (skill: string) => void;
  readonly undo: () => void;
  readonly apply: () => void;
}

const none: SkillPointMap = {};

export const sumOf = (m: SkillPointMap) => Object.values(m).reduce((a, b) => a + b, 0);

export function useSkillPoints(socket: Pick<GameSocket, 'request'>): SkillPoints {
  const skills = character.use((s) => s.character?.sheet?.skills);
  const unspent = skills?.unspent ?? 0;
  const draft = useStaged(unspent, none, ERR.SKILL_POINTS_CHANGED);
  const left = unspent - sumOf(draft.staged);
  return {
    unspent,
    invested: skills?.invested ?? none,
    staged: draft.staged,
    left,
    busy: draft.busy,
    notice: draft.notice,
    stage: (skill) => {
      if (left > 0) {
        draft.stage({ ...draft.staged, [skill]: (draft.staged[skill] ?? 0) + 1 });
      }
    },
    undo: draft.undo,
    apply: () => {
      if (sumOf(draft.staged) > 0) {
        draft.send(() =>
          socket.request('spendSkillPoints', { expectedUnspent: unspent, points: draft.staged }),
        );
      }
    },
  };
}
