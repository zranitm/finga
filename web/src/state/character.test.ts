import { create } from '@bufbuild/protobuf';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { expCurve } from '@/gamedata/exp-curve';
import { skillById, stageAt } from '@/progression/skills';
import {
  CharacterViewSchema,
  ExpGainedSchema,
  LevelUpSchema,
  PromotedSchema,
  StatSheetSchema,
  StatsChangedSchema,
  WelcomeSchema,
} from '@/protocol/messages';
import { resetStores } from '@/testing/reset-stores';

import { character, expToNextLevel } from './character';

const ayla = create(CharacterViewSchema, {
  id: 'c1',
  name: 'Ayla',
  class: 'warrior',
  level: 1,
  exp: 10n,
  stats: { str: 12, dex: 5, int: 4, agi: 5, vit: 10 },
  hp: 204,
  maxHp: 204,
});

const welcome = create(WelcomeSchema, { token: 't', character: ayla, mapId: 'town', tick: 1 });

const gained = (amount: bigint, exp: bigint) => create(ExpGainedSchema, { amount, exp });

describe('character store', () => {
  beforeEach(() => {
    resetStores();
  });

  it.each([
    { level: 1, want: 20 },
    { level: 2, want: 65 },
    { level: 29, want: 6125 },
    { level: 30, want: null },
    { level: 31, want: null },
  ])(
    'level $level needs $want EXP for the next level, from the generated curve',
    ({ level, want }) => {
      expect(expToNextLevel(level)).toBe(want);
      if (want !== null) {
        expect(expCurve.exp_to_next.find((e) => e.level === level)?.exp).toBe(want);
      }
    },
  );

  it('Welcome sets the character and the EXP to the next level', () => {
    character.getState().applyWelcome(welcome);
    expect(character.getState().character).toEqual(ayla);
    expect(character.getState().expToNext).toBe(20);
  });

  it('a Welcome without a character clears it', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyWelcome(create(WelcomeSchema, { token: 't' }));
    expect(character.getState()).toMatchObject({ character: null, expToNext: null });
  });

  it('ExpGained sets exp to the payload total, up or down (the death penalty)', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyExpGained(gained(5n, 15n));
    expect(character.getState().character?.exp).toBe(15n);
    character.getState().applyExpGained(gained(-1n, 14n));
    expect(character.getState().character?.exp).toBe(14n);
  });

  it('LevelUp replaces the character and moves the EXP target', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyLevelUp(
      create(LevelUpSchema, {
        character: {
          ...ayla,
          level: 2,
          exp: 3n,
          stats: { ...ayla.stats!, str: 15, vit: 12 },
          hp: 252,
          maxHp: 252,
        },
      }),
    );
    expect(character.getState().character).toMatchObject({ level: 2, exp: 3n, hp: 252 });
    expect(character.getState().expToNext).toBe(65);
    character.getState().applyLevelUp(create(LevelUpSchema)); // no character: nothing to do
    expect(character.getState().character?.level).toBe(2);
  });

  it('Promoted replaces the character with the new class, at full HP', () => {
    character.getState().applyWelcome(welcome);
    character
      .getState()
      .applyPromoted(
        create(PromotedSchema, { character: { ...ayla, class: 'warrior', hp: 300, maxHp: 300 } }),
      );
    expect(character.getState().character).toMatchObject({ class: 'warrior', hp: 300 });
    character.getState().applyPromoted(create(PromotedSchema)); // no character: nothing to do
    expect(character.getState().character?.class).toBe('warrior');
  });

  it('ExpGained records the change for the HUD, each with a new counter; a negative one is the death penalty', () => {
    character.getState().applyWelcome(welcome);
    character.getState().applyExpGained(gained(5n, 15n));
    expect(character.getState().lastExpChange).toEqual({ amount: 5, seq: 1 });
    character.getState().applyExpGained(gained(-1n, 14n));
    expect(character.getState().lastExpChange).toEqual({ amount: -1, seq: 2 });
    expect(character.getState().character?.exp).toBe(14n);
  });

  it('dead is set and cleared only on a change, and Welcome and clear reset it', () => {
    character.getState().applyWelcome(welcome);
    let changes = 0;
    const off = character.subscribe(() => changes++);
    character.getState().setDead(true);
    character.getState().setDead(true);
    expect(character.getState().dead).toBe(true);
    expect(changes).toBe(1);
    character.getState().setDead(false);
    expect(changes).toBe(2);
    off();
    character.getState().setDead(true);
    character.getState().applyWelcome(welcome);
    expect(character.getState().dead).toBe(false);
    character.getState().setDead(true);
    character.getState().clear();
    expect(character.getState().dead).toBe(false);
    expect(character.getState().lastExpChange).toBeNull();
  });

  it('StatsChanged replaces the sheet and the vitals, so the HUD and the character window show the new maxima', () => {
    character.getState().applyWelcome(welcome);
    const sheet = {
      primary: { str: 13, dex: 5, int: 4, agi: 5, vit: 10 },
      primaryWorn: { str: 1 },
      derived: { physicalAttack: 31.5, physicalDefense: 4, maxHp: 234 },
      derivedWorn: { physicalAttack: 11.5, physicalDefense: 4, maxHp: 30 },
    };
    character
      .getState()
      .applyStatsChanged(create(StatsChangedSchema, { sheet, hp: 204, maxHp: 234 }));
    const c = character.getState().character;
    expect(c).toMatchObject({ hp: 204, maxHp: 234, level: 1, exp: 10n });
    expect(c?.sheet?.derivedWorn?.physicalAttack).toBe(11.5);
    expect(c?.sheet?.primary?.str).toBe(13);
    expect(c?.stats).toMatchObject({ str: 12, dex: 5, int: 4, agi: 5, vit: 10 }); // own: less the gear's STR
    character.getState().applyStatsChanged(
      create(StatsChangedSchema, {
        sheet: { ...sheet, primary: { ...sheet.primary, str: 15 }, primarySkills: { str: 2 } },
        hp: 204,
        maxHp: 234,
      }),
    );
    expect(character.getState().character?.stats?.str).toBe(12); // and less what the skills add
    // Unequipping below the current HP: the server lowered it, and the store takes its word.
    character
      .getState()
      .applyStatsChanged(create(StatsChangedSchema, { sheet, hp: 180, maxHp: 180 }));
    expect(character.getState().character).toMatchObject({ hp: 180, maxHp: 180 });
    // Regen or a potion: HP only, and the sheet and own stats stay.
    character.getState().applyStatsChanged(create(StatsChangedSchema, { hp: 190, maxHp: 180 }));
    const regen = character.getState().character;
    expect(regen).toMatchObject({ hp: 190, maxHp: 180 });
    expect(regen?.sheet?.primary?.str).toBe(13);
    expect(regen?.stats).toMatchObject({ str: 12, vit: 10 });
    character.getState().clear();
    character.getState().applyStatsChanged(create(StatsChangedSchema, { sheet, hp: 1 })); // no character: nothing to do
    expect(character.getState().character).toBeNull();
  });

  it('HP changes only when it differs, and clear empties the store', () => {
    character.getState().applyWelcome(welcome);
    const before = character.getState().character;
    character.getState().setHp(204);
    expect(character.getState().character).toBe(before);
    character.getState().setHp(100);
    expect(character.getState().character?.hp).toBe(100);
    character.getState().clear();
    expect(character.getState().character).toBeNull();
    expect(character.getState().expToNext).toBeNull();
    character.getState().applyExpGained(gained(1n, 1n)); // no character: nothing to do
    expect(character.getState().character).toBeNull();
  });

  describe('the buffs on the sheet (frame-icons.md section 4)', () => {
    let now = 1000;
    beforeEach(() => {
      vi.spyOn(performance, 'now').mockImplementation(() => now);
    });
    afterEach(() => {
      vi.restoreAllMocks();
    });
    const sheet = (...buffs: { skill: string; left: number; points?: number; from?: string }[]) =>
      create(StatSheetSchema, {
        buffs: buffs.map((b) => ({
          skill: b.skill,
          leftMs: BigInt(b.left),
          points: b.points ?? 1,
          from: b.from ?? '',
        })),
      });
    const changed = (s: ReturnType<typeof sheet> | undefined) =>
      character
        .getState()
        .applyStatsChanged(create(StatsChangedSchema, { hp: 1, maxHp: 1, ...(s && { sheet: s }) }));
    const whole = skillById('iron_body')!.stages[0]!.duration_ms;
    const warCry = skillById('war_cry')!;

    it('a Welcome’s sheet lists them, each ending its left_ms after it came; one joined part-way sweeps from its whole duration', () => {
      character.getState().applyWelcome(
        create(WelcomeSchema, {
          character: {
            ...ayla,
            sheet: sheet(
              { skill: 'iron_body', left: 4000 },
              { skill: 'war_cry', left: 9000, points: 4, from: 'c2' },
            ),
          },
        }),
      );
      expect(character.getState().buffs).toEqual([
        { skill: 'iron_body', points: 1, giver: '', from: 5000 - whole, until: 5000 },
        {
          skill: 'war_cry',
          points: 4,
          giver: 'c2',
          from: 10000 - warCry.stages[stageAt(warCry, 4)]!.duration_ms,
          until: 10000,
        },
      ]);
    });

    it('a later sheet keeps a buff’s times when it’s the same use, give or take the network, and starts one used again over', () => {
      character.getState().applyWelcome(welcome);
      changed(sheet({ skill: 'iron_body', left: whole }));
      const first = character.getState().buffs[0]!;
      expect(first).toEqual({
        skill: 'iron_body',
        points: 1,
        giver: '',
        from: 1000,
        until: 1000 + whole,
      });
      now = 3000;
      changed(sheet({ skill: 'iron_body', left: whole - 2100 })); // a sheet for gear, 100 ms late
      expect(character.getState().buffs[0]).toBe(first);
      now = 12000;
      changed(sheet({ skill: 'iron_body', left: whole })); // used again
      expect(character.getState().buffs).toEqual([
        { skill: 'iron_body', points: 1, giver: '', from: 12000, until: 12000 + whole },
      ]);
    });

    it('keeps a party member’s apart from the player’s own, by who gave it', () => {
      character.getState().applyWelcome(welcome);
      changed(sheet({ skill: 'iron_body', left: 5000 }));
      const own = character.getState().buffs[0]!;
      changed(sheet({ skill: 'iron_body', left: 5000, from: 'c2' }));
      expect(character.getState().buffs[0]).not.toBe(own);
      expect(character.getState().buffs[0]!.giver).toBe('c2');
    });

    it('a StatsChanged without a sheet leaves them; a sheet with none, a LevelUp’s or a clear ends them', () => {
      character.getState().applyWelcome(welcome);
      changed(sheet({ skill: 'iron_body', left: 5000 }));
      changed(undefined);
      expect(character.getState().buffs).toHaveLength(1);
      character
        .getState()
        .applyLevelUp(create(LevelUpSchema, { character: { ...ayla, level: 2, sheet: sheet() } }));
      expect(character.getState().buffs).toEqual([]);
      changed(sheet({ skill: 'iron_body', left: 5000 }));
      character.getState().clear();
      expect(character.getState().buffs).toEqual([]);
    });
  });
});
