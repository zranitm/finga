// The client picks the potion and shows what it restores; the world checks every use again.
import { items } from '@/gamedata/items';

export type Vital = 'hp' | 'mp';

export interface Vitals {
  readonly hp: number;
  readonly maxHp: number;
  readonly mp: number;
  readonly maxMp: number;
}

/** HP and MP; a potion restores zero of a vital it doesn't heal. */
export interface Restore {
  readonly hp: number;
  readonly mp: number;
}

interface Template {
  readonly id: string;
  readonly tab: string;
  readonly heal_hp?: number;
  readonly heal_mp?: number;
}

const templates: readonly Template[] = items;

/** What a potion restores at most, or undefined for a template that isn't one: the server's Usable. */
export function healOf(template: string): Restore | undefined {
  const t = templates.find((it) => it.id === template);
  const heal = { hp: t?.heal_hp ?? 0, mp: t?.heal_mp ?? 0 };
  return t?.tab === 'use' && (heal.hp > 0 || heal.mp > 0) ? heal : undefined;
}

export function missing(vital: Vital, v: Vitals): number {
  return Math.max(0, vital === 'hp' ? v.maxHp - v.hp : v.maxMp - v.mp);
}

/** The smallest potion carried that fills what is missing, else the largest; undefined with none. */
export function pickPotion<T extends { readonly template: string }>(
  vital: Vital,
  need: number,
  useTab: readonly (T | null)[],
): T | undefined {
  let fills: { item: T; heal: number } | undefined;
  let largest: { item: T; heal: number } | undefined;
  for (const item of useTab) {
    const heal = item ? (healOf(item.template)?.[vital] ?? 0) : 0;
    if (!item || heal === 0) {
      continue;
    }
    if (heal >= need && (!fills || heal < fills.heal)) {
      fills = { item, heal };
    }
    if (!largest || heal > largest.heal) {
      largest = { item, heal };
    }
  }
  return (fills ?? largest)?.item;
}

/** What a potion restores now: its heal, held to what is missing. */
export function restores(template: string, v: Vitals): Restore {
  const heal = healOf(template) ?? { hp: 0, mp: 0 };
  return { hp: Math.min(heal.hp, missing('hp', v)), mp: Math.min(heal.mp, missing('mp', v)) };
}

/** The full vital a potion would be refused for, as the world refuses it; null when it would restore something. */
export function fullFor(template: string, v: Vitals): Vital | null {
  const heal = healOf(template);
  const r = restores(template, v);
  if (!heal || r.hp > 0 || r.mp > 0) {
    return null;
  }
  return heal.hp > 0 ? 'hp' : 'mp';
}
