// The thirteen stats equipment can roll, in the order every card lists them.
import type { ItemStats } from '@/protocol/messages';

export type StatKey = keyof Omit<ItemStats, '$typeName' | '$unknown'>;

export interface StatInfo {
  /** The field on the wire. */
  readonly key: StatKey;
  /** The name in items.yaml's ranges. */
  readonly data: string;
  readonly label: string;
  /** In tenths of a percent. */
  readonly rate: boolean;
}

export const itemStats: readonly StatInfo[] = [
  { key: 'attack', data: 'attack', label: 'Attack', rate: false },
  { key: 'defense', data: 'defense', label: 'Defense', rate: false },
  { key: 'str', data: 'str', label: 'STR', rate: false },
  { key: 'dex', data: 'dex', label: 'DEX', rate: false },
  { key: 'int', data: 'int', label: 'INT', rate: false },
  { key: 'agi', data: 'agi', label: 'AGI', rate: false },
  { key: 'vit', data: 'vit', label: 'VIT', rate: false },
  { key: 'accuracy', data: 'accuracy', label: 'Accuracy', rate: false },
  { key: 'evasion', data: 'evasion', label: 'Evasion', rate: false },
  { key: 'critRate', data: 'crit_rate', label: 'Crit rate', rate: true },
  { key: 'critDamage', data: 'crit_damage', label: 'Crit damage', rate: true },
  { key: 'maxHp', data: 'max_hp', label: 'Max HP', rate: false },
  { key: 'maxMp', data: 'max_mp', label: 'Max MP', rate: false },
];

/** A stat as a card reads it: a rate's tenths of a percent become a percent. */
export function statText(info: StatInfo, value: number): string {
  return info.rate ? `${value / 10}%` : String(value);
}
