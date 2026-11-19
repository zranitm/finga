import { create } from '@bufbuild/protobuf';
import { beforeEach, describe, expect, it } from 'vitest';

import {
  CharacterViewSchema,
  ExpGainedSchema,
  LevelUpSchema,
  PromotedSchema,
  LootKind,
  LootSchema,
} from '@/protocol/messages';

import { MAX_COMBAT_LINES, combatLog } from './combat-log';

const loot = (kind: LootKind, items = [{ template: 'slime_gel', quantity: 2 }]) =>
  create(LootSchema, {
    kind,
    mapId: 'meadow',
    pouchId: 7,
    ownerId: 'c-ayla',
    ownerName: 'Ayla',
    items,
  });
const exp = (amount: bigint) => create(ExpGainedSchema, { amount, exp: 40n });

describe('combatLog', () => {
  beforeEach(() => {
    combatLog.reset();
  });

  it('EXP, a level-up, a promotion, loot and a potion each add a line, oldest first', () => {
    const s = combatLog.getState();
    s.applyExpGained(exp(12n));
    s.applyExpGained(exp(-30n));
    s.applyLevelUp(create(LevelUpSchema, { character: create(CharacterViewSchema, { level: 5 }) }));
    s.applyPromoted(
      create(PromotedSchema, { character: create(CharacterViewSchema, { class: 'thief' }) }),
    );
    s.applyLoot(loot(LootKind.TAKEN));
    s.applyLoot(
      loot(LootKind.LEFT, [
        { template: 'red_potion', quantity: 1 },
        { template: 'slime_gel', quantity: 2 },
      ]),
    );
    s.drank('red_potion', 50);
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'exp', amount: 12 },
      { kind: 'exp', amount: -30 },
      { kind: 'level', level: 5 },
      { kind: 'promoted', cls: 'thief' },
      {
        kind: 'loot',
        loot: LootKind.TAKEN,
        ownerId: 'c-ayla',
        ownerName: 'Ayla',
        items: [{ template: 'slime_gel', quantity: 2 }],
      },
      {
        kind: 'loot',
        loot: LootKind.LEFT,
        items: [
          { template: 'red_potion', quantity: 1 },
          { template: 'slime_gel', quantity: 2 },
        ],
      },
      { kind: 'drank', template: 'red_potion', hp: 50 },
    ]);
    const ids = combatLog.getState().lines.map((l) => l.id);
    expect(new Set(ids).size).toBe(7);
  });

  it.each([
    { name: 'no EXP', apply: () => combatLog.getState().applyExpGained(exp(0n)) },
    {
      name: 'a LevelUp with no character',
      apply: () => combatLog.getState().applyLevelUp(create(LevelUpSchema, {})),
    },
    {
      name: 'a Promoted with no character',
      apply: () => combatLog.getState().applyPromoted(create(PromotedSchema, {})),
    },
    {
      name: 'a Loot with no kind',
      apply: () => combatLog.getState().applyLoot(loot(LootKind.UNSPECIFIED)),
    },
    {
      name: 'a Loot of a kind from a newer server',
      apply: () => combatLog.getState().applyLoot(loot(9 as LootKind)),
    },
    {
      name: 'a Loot naming no item',
      apply: () => combatLog.getState().applyLoot(loot(LootKind.TAKEN, [])),
    },
  ])('$name adds nothing', ({ apply }) => {
    apply();
    expect(combatLog.getState().lines).toEqual([]);
  });

  it('a skill whose points put it in a later stage evolves, in a line naming both forms', () => {
    const s = combatLog.getState();
    s.applySkillPoints({ strike: 8, throw: 30 }, { strike: 9, throw: 31 });
    expect(combatLog.getState().lines).toEqual([]);
    s.applySkillPoints({ strike: 9 }, { strike: 12, spark: 25, second_wind: 1, gone: 99 });
    expect(combatLog.getState().lines).toMatchObject([
      { kind: 'evolved', from: 'Strike', to: 'Heavy Strike' },
      { kind: 'evolved', from: 'Spark', to: 'Arc' },
    ]);
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

  it('clear forgets the lines', () => {
    const s = combatLog.getState();
    s.applyLoot(loot(LootKind.TAKEN));
    s.clear();
    expect(combatLog.getState().lines).toEqual([]);
  });
});
