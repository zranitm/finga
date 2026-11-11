import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

import { describe, expect, it } from 'vitest';

import { classes } from '@/gamedata/classes';

import {
  activeRoll,
  addStats,
  autoAssign,
  derivedStats,
  hasSplit,
  meets,
  sheetOf,
  statsShort,
  total,
} from './stats';
import type { Primary, Roll } from './stats';

// The vectors are the contract with the server's DeriveStats; they travel under the data-file names.
interface Vector {
  readonly name: string;
  readonly class: string;
  readonly level: number;
  readonly points: Primary;
  readonly worn: Readonly<Record<string, number>>;
  readonly expected: Readonly<
    Record<
      'primary' | 'primary_worn' | 'derived' | 'derived_worn',
      Readonly<Record<string, number>>
    >
  >;
}

const vectorsDir = path.resolve(process.cwd(), '..', 'server', 'testdata', 'stats');
const vectors = readdirSync(vectorsDir)
  .filter((name) => name.endsWith('.json'))
  .map((name) => JSON.parse(readFileSync(path.join(vectorsDir, name), 'utf8')) as Vector);

const byData = Object.fromEntries(derivedStats.map((s) => [s.data, s.key]));
const roll = (worn: Vector['worn']): Roll =>
  Object.fromEntries(Object.entries(worn).map(([k, v]) => [byData[k] ?? k, v]));
const base: Primary = classes[0].base_stats; // every character starts with the Beginner's

describe('the stat vectors', () => {
  it('are all here', () => {
    expect(vectors.map((v) => v.name)).toEqual(
      expect.arrayContaining(['warrior-level-1', 'warrior-every-stat-worn', 'magic-build']),
    );
  });

  it.each(vectors.map((v) => [v.name, v] as const))('%s matches the server', (_, v) => {
    const got = sheetOf(v.class, v.level, addStats(base, v.points), roll(v.worn));
    expect(got.primary).toEqual(v.expected.primary);
    expect(got.primaryWorn).toEqual(v.expected.primary_worn);
    for (const { key, data } of derivedStats) {
      expect(got.derived[key], `derived ${data}`).toBeCloseTo(v.expected.derived[data] ?? 0, 9);
      expect(got.derivedWorn[key], `worn ${data}`).toBeCloseTo(
        v.expected.derived_worn[data] ?? 0,
        9,
      );
    }
  });
});

describe('requirements', () => {
  const own: Primary = { str: 20, dex: 5, int: 4, agi: 5, vit: 10 };
  const axe = { min_level: 10, class: 'warrior', requires: { str: 25, dex: 5 } };

  it('name each stat the own ones fall short of', () => {
    expect(statsShort(axe, own)).toEqual(['str']);
    expect(statsShort(axe, { ...own, str: 25 })).toEqual([]);
    expect(statsShort({}, own)).toEqual([]);
  });

  it('are met only with the level, the class and every stat', () => {
    const strong = { ...own, str: 25 };
    expect(meets(axe, 10, 'warrior', strong)).toBe(true);
    expect(meets(axe, 9, 'warrior', strong)).toBe(false);
    expect(meets(axe, 10, 'bowman', strong)).toBe(false);
    expect(meets(axe, 10, 'warrior', own)).toBe(false);
  });

  it('leave an unmet item out of what is worn, without counting gear toward them', () => {
    const worn = [
      { template: { requires: { str: 21 } }, roll: { str: 5, physicalAttack: 10 } },
      { template: { min_level: 1 }, roll: { str: 2, physicalDefense: 3 } },
    ];
    const only = (r: Roll) => Object.fromEntries(Object.entries(r).filter(([, v]) => v !== 0));
    expect(only(activeRoll(worn, 10, 'warrior', own))).toEqual({ str: 2, physicalDefense: 3 });
    expect(only(activeRoll(worn, 10, 'warrior', { ...own, str: 21 }))).toEqual({
      str: 7,
      physicalAttack: 10,
      physicalDefense: 3,
    });
  });
});

describe('autoAssign', () => {
  it.each([
    { name: "a level's points follow the split", cls: 'warrior', n: 5, want: { str: 3, vit: 2 } },
    { name: 'two levels double it', cls: 'bowman', n: 10, want: { str: 4, dex: 4, agi: 2 } },
    { name: 'a part follows it stat by stat', cls: 'warrior', n: 9, want: { str: 6, vit: 3 } },
    { name: 'none gives nothing', cls: 'warrior', n: 0, want: {} },
  ])('$name', ({ cls, n, want }) => {
    const got = autoAssign(cls, n);
    expect(got).toEqual({ str: 0, dex: 0, int: 0, agi: 0, vit: 0, ...want });
    expect(total(got)).toBe(n);
  });

  it('every class a Beginner is promoted to has a split of one level; the Beginner has none', () => {
    for (const c of classes.slice(1)) {
      expect(hasSplit(c.id), c.id).toBe(true);
      expect(total(autoAssign(c.id, 5)), c.id).toBe(5);
    }
    expect(hasSplit('beginner')).toBe(false);
    expect(total(autoAssign('beginner', 5))).toBe(0);
  });
});
