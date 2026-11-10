// The twenty-one stats equipment can roll, in the order every card lists them.
import type { ItemStats } from '@/protocol/messages';

export type StatKey = keyof Omit<ItemStats, '$typeName' | '$unknown'>;

/** whole as rolled; percent in tenths of a percent; perSecond in tenths of a point a second. */
export type Unit = 'whole' | 'percent' | 'perSecond';

export interface StatInfo {
  /** The field on the wire. */
  readonly key: StatKey;
  /** The name in items.yaml's ranges. */
  readonly data: string;
  readonly label: string;
  readonly unit: Unit;
}

export const itemStats: readonly StatInfo[] = [
  { key: 'physicalAttack', data: 'physical_attack', label: 'Physical attack', unit: 'whole' },
  { key: 'magicAttack', data: 'magic_attack', label: 'Magic attack', unit: 'whole' },
  { key: 'physicalDefense', data: 'physical_defense', label: 'Physical defense', unit: 'whole' },
  { key: 'magicDefense', data: 'magic_defense', label: 'Magic defense', unit: 'whole' },
  { key: 'str', data: 'str', label: 'STR', unit: 'whole' },
  { key: 'dex', data: 'dex', label: 'DEX', unit: 'whole' },
  { key: 'int', data: 'int', label: 'INT', unit: 'whole' },
  { key: 'agi', data: 'agi', label: 'AGI', unit: 'whole' },
  { key: 'vit', data: 'vit', label: 'VIT', unit: 'whole' },
  { key: 'maxHp', data: 'max_hp', label: 'Max HP', unit: 'whole' },
  { key: 'maxMp', data: 'max_mp', label: 'Max MP', unit: 'whole' },
  { key: 'accuracy', data: 'accuracy', label: 'Accuracy', unit: 'whole' },
  { key: 'evasion', data: 'evasion', label: 'Evasion', unit: 'whole' },
  { key: 'critRate', data: 'crit_rate', label: 'Crit rate', unit: 'percent' },
  { key: 'critDamage', data: 'crit_damage', label: 'Crit damage', unit: 'percent' },
  { key: 'penetration', data: 'penetration', label: 'Penetration', unit: 'percent' },
  { key: 'hpRegen', data: 'hp_regen', label: 'HP regen', unit: 'perSecond' },
  { key: 'mpRegen', data: 'mp_regen', label: 'MP regen', unit: 'perSecond' },
  { key: 'attackSpeed', data: 'attack_speed', label: 'Attack speed', unit: 'percent' },
  { key: 'cooldownSpeed', data: 'cooldown_speed', label: 'Cooldown speed', unit: 'percent' },
  { key: 'moveSpeed', data: 'move_speed', label: 'Move speed', unit: 'percent' },
];

/** A stat as a card reads it: tenths become a percent, or a point a second. */
export function statText(info: StatInfo, value: number): string {
  switch (info.unit) {
    case 'percent':
      return `${value / 10}%`;
    case 'perSecond':
      return `${value / 10}/s`;
    default:
      return String(value);
  }
}
