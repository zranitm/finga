// Mirrors server/internal/progression for the character window's preview; testdata/stats pins the two together, and the server's numbers win.
import { classes } from '@/gamedata/classes';
import { stats } from '@/gamedata/stats';

import { fits } from './classes';

export const primaryKeys = ['str', 'dex', 'int', 'agi', 'vit'] as const;
export type PrimaryKey = (typeof primaryKeys)[number];
export type Primary = Readonly<Record<PrimaryKey, number>>;

/** The sixteen derived stats, each with its name in the data files. */
export const derivedStats = [
  { key: 'maxHp', data: 'max_hp' },
  { key: 'maxMp', data: 'max_mp' },
  { key: 'physicalAttack', data: 'physical_attack' },
  { key: 'magicAttack', data: 'magic_attack' },
  { key: 'physicalDefense', data: 'physical_defense' },
  { key: 'magicDefense', data: 'magic_defense' },
  { key: 'accuracy', data: 'accuracy' },
  { key: 'evasion', data: 'evasion' },
  { key: 'critRate', data: 'crit_rate' },
  { key: 'critDamage', data: 'crit_damage' },
  { key: 'penetration', data: 'penetration' },
  { key: 'hpRegen', data: 'hp_regen' },
  { key: 'mpRegen', data: 'mp_regen' },
  { key: 'attackSpeed', data: 'attack_speed' },
  { key: 'cooldownSpeed', data: 'cooldown_speed' },
  { key: 'moveSpeed', data: 'move_speed' },
] as const;
export type DerivedKey = (typeof derivedStats)[number]['key'];
type DerivedData = (typeof derivedStats)[number]['data'];
export type Derived = Readonly<Record<DerivedKey, number>>;

/** What gear gives, all 21 in whole numbers: rates, crit damage and penetration in tenths of a percent, regen in tenths of a point a second. */
export type Roll = Readonly<Partial<Record<PrimaryKey | DerivedKey, number>>>;

export interface Sheet {
  readonly primary: Primary;
  readonly primaryWorn: Primary;
  readonly derived: Derived;
  readonly derivedWorn: Derived;
}

interface Grows {
  readonly base?: number;
  readonly per_level?: number;
}

interface Curve {
  readonly stat: PrimaryKey;
  readonly base?: number;
  readonly limit: number;
  readonly k: number;
}

interface Rules {
  readonly points_per_level: number;
  readonly per_point: Readonly<Record<PrimaryKey, Readonly<Partial<Record<DerivedData, number>>>>>;
  readonly diminishing: Readonly<
    Record<'crit_rate' | 'attack_speed' | 'cooldown_speed' | 'move_speed', Curve>
  >;
  readonly limits: { readonly penetration: number };
  readonly classes: Readonly<Record<string, Readonly<Partial<Record<DerivedData, Grows>>>>>;
}

const rules: Rules = stats;

const tenthsOfAPercent = 1000;
const tenthsOfAPoint = 10;

export const pointsPerLevel = rules.points_per_level;

export const noPoints: Primary = { str: 0, dex: 0, int: 0, agi: 0, vit: 0 };

export function addStats(a: Primary, b: Partial<Primary>): Primary {
  return {
    str: a.str + (b.str ?? 0),
    dex: a.dex + (b.dex ?? 0),
    int: a.int + (b.int ?? 0),
    agi: a.agi + (b.agi ?? 0),
    vit: a.vit + (b.vit ?? 0),
  };
}

export function total(p: Partial<Primary>): number {
  return primaryKeys.reduce((n, k) => n + (p[k] ?? 0), 0);
}

/** A class's derived stats at a level, from its own primary stats and the gear it wears; gear STR counts like spent STR. */
export function deriveStats(cls: string, level: number, own: Primary, worn: Roll): Derived {
  const p = addStats(own, worn);
  const base = rules.classes[cls] ?? {};
  const gear = (k: DerivedKey) => worn[k] ?? 0;
  const flat = (name: DerivedData): number => {
    const g = base[name];
    let v = (g?.base ?? 0) + (g?.per_level ?? 0) * (level - 1);
    for (const k of primaryKeys) {
      v += (rules.per_point[k][name] ?? 0) * p[k];
    }
    return v;
  };
  const rate = (c: Curve, from: number): number => {
    const points = Math.max(p[c.stat], 0);
    const b = c.base ?? 0;
    return Math.min(
      b + ((c.limit - b) * points) / (points + c.k) + from / tenthsOfAPercent,
      c.limit,
    );
  };
  const d = rules.diminishing;
  return {
    maxHp: Math.floor(flat('max_hp')) + gear('maxHp'),
    maxMp: Math.floor(flat('max_mp')) + gear('maxMp'),
    physicalAttack: flat('physical_attack') + gear('physicalAttack'),
    magicAttack: flat('magic_attack') + gear('magicAttack'),
    physicalDefense: flat('physical_defense') + gear('physicalDefense'),
    magicDefense: flat('magic_defense') + gear('magicDefense'),
    accuracy: flat('accuracy') + gear('accuracy'),
    evasion: flat('evasion') + gear('evasion'),
    critRate: rate(d.crit_rate, gear('critRate')),
    critDamage: flat('crit_damage') + gear('critDamage') / tenthsOfAPercent,
    penetration: Math.min(gear('penetration') / tenthsOfAPercent, rules.limits.penetration),
    hpRegen: flat('hp_regen') + gear('hpRegen') / tenthsOfAPoint,
    mpRegen: flat('mp_regen') + gear('mpRegen') / tenthsOfAPoint,
    attackSpeed: rate(d.attack_speed, gear('attackSpeed')),
    cooldownSpeed: rate(d.cooldown_speed, gear('cooldownSpeed')),
    moveSpeed: rate(d.move_speed, gear('moveSpeed')),
  };
}

/** The totals, and the equipment's share of each: the totals less those without it. */
export function sheetOf(cls: string, level: number, own: Primary, worn: Roll): Sheet {
  const withGear = deriveStats(cls, level, own, worn);
  const without = deriveStats(cls, level, own, {});
  const primaryWorn = addStats(noPoints, worn);
  const derivedWorn = Object.fromEntries(
    derivedStats.map(({ key }) => [key, withGear[key] - without[key]]),
  ) as Record<DerivedKey, number>;
  return { primary: addStats(own, worn), primaryWorn, derived: withGear, derivedWorn };
}

const rollKeys: readonly (PrimaryKey | DerivedKey)[] = [
  ...primaryKeys,
  ...derivedStats.map((s) => s.key),
];

/** What an item asks of its wearer, as items.yaml writes it. */
export interface Requirements {
  readonly min_level?: number;
  readonly class?: string;
  readonly requires?: Readonly<Partial<Record<PrimaryKey, number>>>;
}

/** The stats a character's own ones fall short of; gear never counts, so one item's bonus never unlocks another. */
export function statsShort(r: Requirements, own: Primary): PrimaryKey[] {
  return primaryKeys.filter((k) => own[k] < (r.requires?.[k] ?? 0));
}

/** Whether a character meets every requirement: the level, the class for a weapon (its own or one above it), and each stat minimum against its own stats. */
export function meets(r: Requirements, level: number, cls: string, own: Primary): boolean {
  return level >= (r.min_level ?? 0) && fits(r.class, cls) && statsShort(r, own).length === 0;
}

/** The rolls of the worn items whose requirements are met summed; an item below its requirement stays worn and adds nothing. */
export function activeRoll(
  worn: readonly { readonly template: Requirements; readonly roll: Roll }[],
  level: number,
  cls: string,
  own: Primary,
): Roll {
  const sum: Partial<Record<PrimaryKey | DerivedKey, number>> = {};
  for (const w of worn) {
    if (!meets(w.template, level, cls, own)) {
      continue;
    }
    for (const k of rollKeys) {
      sum[k] = (sum[k] ?? 0) + (w.roll[k] ?? 0);
    }
  }
  return sum;
}

/** Whether the class has a recommended split; the Beginner has none, so it spends its own points. */
export function hasSplit(cls: string): boolean {
  const split: Partial<Primary> = classes.find((c) => c.id === cls)?.auto_assign ?? {};
  return primaryKeys.some((k) => (split[k] ?? 0) > 0);
}

/** n points by the class's recommended split: whole splits first, then the rest stat by stat. */
export function autoAssign(cls: string, n: number): Primary {
  const split: Partial<Primary> = classes.find((c) => c.id === cls)?.auto_assign ?? {};
  const whole = Math.floor(n / pointsPerLevel);
  let left = n - whole * pointsPerLevel;
  const out: Record<PrimaryKey, number> = { ...noPoints };
  for (const k of primaryKeys) {
    const share = split[k] ?? 0;
    const extra = Math.min(share, left);
    out[k] = share * whole + extra;
    left -= extra;
  }
  return out;
}
