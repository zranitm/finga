import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { loot as timings } from '@/gamedata/loot';
import {
  CharacterViewSchema,
  ExpGainedSchema,
  LevelUpSchema,
  LootKind,
  LootSchema,
} from '@/protocol/messages';

import { MAX_COMBAT_LINES, combatLog } from './combat-log';

const loot = (kind: LootKind, dropId: number, mapId = 'meadow') =>
  create(LootSchema, {
    kind,
    mapId,
    dropId,
    ownerId: 'c-ayla',
    ownerName: 'Ayla',
    template: 'slime_gel',
    quantity: 2,
  });
const exp = (amount: bigint) => create(ExpGainedSchema, { amount, exp: 40n });

const keys = () => combatLog.getState().waiting.map((w) => w.key);

describe('combatLog', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    combatLog.reset();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('EXP, a level-up, loot and a potion each add a line, oldest first', () => {
    const s = combatLog.getState();
    s.applyExpGained(exp(12n));
    s.applyExpGained(exp(-30n));
    s.applyLevelUp(create(LevelUpSchema, { character: create(CharacterViewSchema, { level: 5 }) }));
    s.applyLoot(loot(LootKind.COLLECTED, 7));
    s.drank('red_potion', { hp: 50, mp: 0 });
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'exp', amount: 12 },
      { kind: 'exp', amount: -30 },
      { kind: 'level', level: 5 },
      {
        kind: 'loot',
        loot: LootKind.COLLECTED,
        ownerId: 'c-ayla',
        ownerName: 'Ayla',
        template: 'slime_gel',
        quantity: 2,
      },
      { kind: 'drank', template: 'red_potion', hp: 50, mp: 0 },
    ]);
    const ids = combatLog.getState().lines.map((l) => l.id);
    expect(new Set(ids).size).toBe(5);
  });

  it.each([
    { name: 'no EXP', apply: () => combatLog.getState().applyExpGained(exp(0n)) },
    {
      name: 'a LevelUp with no character',
      apply: () => combatLog.getState().applyLevelUp(create(LevelUpSchema, {})),
    },
    {
      name: 'a Loot with no kind',
      apply: () => combatLog.getState().applyLoot(loot(LootKind.UNSPECIFIED, 7)),
    },
    {
      name: 'a Loot of a kind from a newer server',
      apply: () => combatLog.getState().applyLoot(loot(9 as LootKind, 7)),
    },
  ])('$name adds nothing', ({ apply }) => {
    apply();
    expect(combatLog.getState().lines).toEqual([]);
  });

  it(`keeps the last ${MAX_COMBAT_LINES} lines`, () => {
    for (let i = 1; i <= MAX_COMBAT_LINES + 5; i++) {
      combatLog.getState().applyExpGained(exp(BigInt(i)));
    }
    const { lines } = combatLog.getState();
    expect(lines).toHaveLength(MAX_COMBAT_LINES);
    expect(lines[0]).toMatchObject({ amount: 6 });
    expect(lines.at(-1)).toMatchObject({ amount: MAX_COMBAT_LINES + 5 });
  });

  it('counts drops waiting for room by map and drop, until collected or lost', () => {
    const s = combatLog.getState();
    s.applyLoot(loot(LootKind.WAITING, 7));
    s.applyLoot(loot(LootKind.WAITING, 8));
    s.applyLoot(loot(LootKind.WAITING, 7, 'cave'));
    s.applyLoot(loot(LootKind.WAITING, 7));
    expect(keys()).toEqual(['meadow/8', 'cave/7', 'meadow/7']);
    s.applyLoot(loot(LootKind.COLLECTED, 7));
    s.applyLoot(loot(LootKind.LOST, 7, 'cave'));
    s.applyLoot(loot(LootKind.COLLECTED, 99));
    expect(keys()).toEqual(['meadow/8']);
  });

  it("forgets a waiting drop at the end of a drop's life, when no word of it came", () => {
    const s = combatLog.getState();
    s.applyLoot(loot(LootKind.WAITING, 7));
    vi.advanceTimersByTime(timings.drop_life_ms / 2);
    s.applyLoot(loot(LootKind.WAITING, 8));
    vi.advanceTimersByTime(timings.drop_life_ms / 2 - 1);
    expect(keys()).toEqual(['meadow/7', 'meadow/8']);
    vi.advanceTimersByTime(1);
    expect(keys()).toEqual(['meadow/8']);
    vi.advanceTimersByTime(timings.drop_life_ms / 2);
    expect(keys()).toEqual([]);
  });

  it('a drop told waiting again, its ID reused after a world restart, waits a full life from then', () => {
    const s = combatLog.getState();
    s.applyLoot(loot(LootKind.WAITING, 7));
    vi.advanceTimersByTime(timings.drop_life_ms - 1);
    s.applyLoot(loot(LootKind.WAITING, 7));
    vi.advanceTimersByTime(1);
    expect(keys()).toEqual(['meadow/7']);
    vi.advanceTimersByTime(timings.drop_life_ms - 1);
    expect(keys()).toEqual([]);
  });

  it('clear forgets the lines and the waiting drops', () => {
    const s = combatLog.getState();
    s.applyLoot(loot(LootKind.WAITING, 7));
    s.clear();
    expect(combatLog.getState()).toMatchObject({ lines: [], waiting: [] });
  });
});
