import { create } from '@bufbuild/protobuf';
import { describe, expect, it } from 'vitest';

import { PromotionConditionKind, PromotionConditionSchema, StatsSchema } from '@/protocol/messages';

import { haveNow } from './promotion';
import type { Trainee } from './promotion';

const ayla: Trainee = {
  level: 12,
  own: create(StatsSchema, { str: 31, dex: 7, int: 7, agi: 9, vit: 7 }),
  invested: { throw: 4 },
  log: {
    bowmans_trial: { status: 'finished', progress: [], timesFinished: 1 },
    thiefs_trial: { status: 'active', progress: [0], timesFinished: 0 },
  },
};

describe('a promotion condition now', () => {
  it.each([
    { name: 'the level', kind: PromotionConditionKind.LEVEL, id: '', want: 12 },
    { name: 'an own stat', kind: PromotionConditionKind.STAT, id: 'agi', want: 9 },
    { name: 'skill points', kind: PromotionConditionKind.SKILL, id: 'throw', want: 4 },
    { name: 'a skill with none', kind: PromotionConditionKind.SKILL, id: 'spark', want: 0 },
    { name: 'a finished quest', kind: PromotionConditionKind.QUEST, id: 'bowmans_trial', want: 1 },
    { name: 'a quest under way', kind: PromotionConditionKind.QUEST, id: 'thiefs_trial', want: 0 },
    { name: 'a stat this client lacks', kind: PromotionConditionKind.STAT, id: 'luk', want: 3 },
    { name: 'a kind this client lacks', kind: 9 as PromotionConditionKind, id: 'x', want: 3 },
  ])('counts $name as the world does', ({ kind, id, want }) => {
    expect(haveNow(create(PromotionConditionSchema, { kind, id, need: 30, have: 3 }), ayla)).toBe(
      want,
    );
  });
});
