import { describe, expect, it } from 'vitest';

import {
  inReach,
  isReady,
  markerFor,
  nearestInReach,
  npcById,
  objectiveCount,
  objectiveLabel,
  offerState,
  questById,
  readyQuests,
} from './quests';
import type { Log, Player, Quest } from './quests';

function player(over: Partial<Player> = {}, held: Record<string, number> = {}): Player {
  return { level: 1, cls: 'warrior', log: {}, held: (t) => held[t] ?? 0, ...over };
}

const quest = (over: Partial<Quest>): Quest => ({
  id: 'q',
  name: 'Q',
  giver: 'town_guide',
  turn_in: 'town_guide',
  level: 1,
  class: null,
  quests: [],
  objectives: [],
  rewards: { exp: 0, items: [] },
  repeatable: false,
  ...over,
});

const cleanup = questById('slime_cleanup')!;
const samples = questById('sticky_samples')!;
const guide = npcById('town_guide')!;
const finishedCleanup: Log = {
  slime_cleanup: { status: 'finished', progress: [], timesFinished: 1 },
};

describe('offerState, as the server’s NpcDialog', () => {
  it('a quest whose conditions are met is available', () => {
    expect(offerState(cleanup, player())).toBe('available');
  });

  it('an active quest is active, whatever its conditions', () => {
    const log: Log = { sticky_samples: { status: 'active', progress: [0], timesFinished: 0 } };
    expect(offerState(samples, player({ log }))).toBe('active');
  });

  it('a one-time quest finished is finished; a repeatable one is available again', () => {
    expect(offerState(cleanup, player({ log: finishedCleanup }))).toBe('finished');
    const again = { ...cleanup, repeatable: true };
    expect(offerState(again, player({ log: finishedCleanup }))).toBe('available');
  });

  it.each([
    { name: 'a level short', p: player({ level: 1, log: finishedCleanup }) },
    { name: 'its quest unfinished', p: player({ level: 2 }) },
  ])('the second quest with $name is unmet', ({ p }) => {
    expect(offerState(samples, p)).toBe('unmet');
  });

  it('a quest for another class is unmet', () => {
    expect(offerState(quest({ class: 'bowman' }), player())).toBe('unmet');
    expect(offerState(quest({ class: 'warrior' }), player())).toBe('available');
  });
});

describe('objective counts and readiness', () => {
  const mixed = quest({
    objectives: [
      { kind: 'kill', target: 'green_slime', count: 2 },
      { kind: 'collect', target: 'slime_gel', count: 3 },
      { kind: 'talk', target: 'town_guide', count: 1 },
      { kind: 'level', target: null, count: 3 },
    ],
  });
  const log: Log = { q: { status: 'active', progress: [2, 0, 1, 0], timesFinished: 0 } };

  it('counts kills and talks from the log, items from the inventory, and the level from the character', () => {
    const p = player({ level: 2, log }, { slime_gel: 2 });
    const entry = log.q!;
    expect(mixed.objectives.map((_, k) => objectiveCount(mixed, entry, k, p))).toEqual([
      2, 2, 1, 2,
    ]);
  });

  it('is ready only once every objective is done, the items held included', () => {
    expect(isReady(mixed, player({ level: 3, log }, { slime_gel: 2 }))).toBe(false);
    expect(isReady(mixed, player({ level: 2, log }, { slime_gel: 3 }))).toBe(false);
    expect(isReady(mixed, player({ level: 3, log }, { slime_gel: 3 }))).toBe(true);
  });

  it('a finished quest, or one out of the log, is not ready', () => {
    expect(isReady(cleanup, player({ log: finishedCleanup }))).toBe(false);
    expect(isReady(cleanup, player())).toBe(false);
  });

  it('readyQuests lists the active quests ready to hand in', () => {
    const p = player(
      {
        level: 2,
        log: {
          slime_cleanup: { status: 'active', progress: [10], timesFinished: 0 },
          sticky_samples: { status: 'active', progress: [0], timesFinished: 0 },
        },
      },
      { slime_gel: 4 },
    );
    expect(readyQuests(p)).toEqual(['slime_cleanup']);
  });

  it('labels each kind of objective', () => {
    expect(mixed.objectives.map(objectiveLabel)).toEqual([
      'Green Slime',
      'Slime Gel',
      'Talk to Town Guide',
      'Reach level 3',
    ]);
  });
});

describe('markerFor', () => {
  it('shows a quest to take, then one under way, then one to hand in', () => {
    expect(markerFor(guide, player())).toBe('available');
    const active: Log = { slime_cleanup: { status: 'active', progress: [3], timesFinished: 0 } };
    expect(markerFor(guide, player({ log: active }))).toBe('active');
    const done: Log = { slime_cleanup: { status: 'active', progress: [10], timesFinished: 0 } };
    expect(markerFor(guide, player({ log: done }))).toBe('ready');
  });

  it('shows nothing once everything it gives is done or out of reach of the conditions', () => {
    expect(markerFor(guide, player({ level: 1, log: finishedCleanup }))).toBeNull();
    expect(markerFor(guide, player({ level: 2, log: finishedCleanup }))).toBe('available');
  });
});

describe('reach, as the server measures it from the character’s middle', () => {
  it('the guide at x 496 is within reach of feet 16 px away, and not of feet 70 px away', () => {
    expect(inReach(guide, 480, 608)).toBe(true);
    expect(inReach(guide, 566, 608)).toBe(false);
    expect(nearestInReach('town', 480, 608)?.id).toBe('town_guide');
    expect(nearestInReach('town', 566, 608)).toBeUndefined();
    expect(nearestInReach('forest', 496, 608)).toBeUndefined();
  });
});
