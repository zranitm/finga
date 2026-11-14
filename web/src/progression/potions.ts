// The client picks the potion and shows what it restores; the world checks every use again.
import { items } from '@/gamedata/items';

export interface Vitals {
  readonly hp: number;
  readonly maxHp: number;
}

interface Template {
  readonly id: string;
  readonly tab: string;
  readonly heal_hp?: number;
}

const templates: readonly Template[] = items;

/** The HP a potion restores at most, or undefined for a template that isn't one: the server's Usable. */
export function healOf(template: string): number | undefined {
  const t = templates.find((it) => it.id === template);
  const heal = t?.heal_hp ?? 0;
  return t?.tab === 'use' && heal > 0 ? heal : undefined;
}

export function missing(v: Vitals): number {
  return Math.max(0, v.maxHp - v.hp);
}

/** The smallest potion carried that fills what is missing, else the largest; undefined with none. */
export function pickPotion<T extends { readonly template: string }>(
  need: number,
  useTab: readonly (T | null)[],
): T | undefined {
  let fills: { item: T; heal: number } | undefined;
  let largest: { item: T; heal: number } | undefined;
  for (const item of useTab) {
    const heal = item ? (healOf(item.template) ?? 0) : 0;
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

/** The HP a potion restores now: its heal, held to what is missing. */
export function restores(template: string, v: Vitals): number {
  return Math.min(healOf(template) ?? 0, missing(v));
}
