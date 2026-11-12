// What a character has toward a trainer's promotion condition, counted as the world counts it, so an open dialog follows spends, level-ups and quests.
import type { Log } from '@/progression/quests';
import { PromotionConditionKind } from '@/protocol/messages';
import type { PromotionCondition, Stats } from '@/protocol/messages';

export interface Trainee {
  readonly level: number;
  /** Own stats, without gear or skills. */
  readonly own: Stats | undefined;
  readonly invested: Readonly<Record<string, number>>;
  readonly log: Log;
}

/** The character's count toward c now; one this client doesn't know keeps the dialog's count. */
export function haveNow(c: PromotionCondition, t: Trainee): number {
  switch (c.kind) {
    case PromotionConditionKind.LEVEL:
      return t.level;
    case PromotionConditionKind.STAT: {
      const v = t.own?.[c.id as keyof Stats];
      return typeof v === 'number' ? v : c.have;
    }
    case PromotionConditionKind.SKILL:
      return t.invested[c.id] ?? 0;
    case PromotionConditionKind.QUEST:
      return (t.log[c.id]?.timesFinished ?? 0) > 0 ? 1 : 0;
    default:
      return c.have;
  }
}
