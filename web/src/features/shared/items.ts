// What the inventory and the character window both say about an item.
import type { ItemTemplate } from '@/components/ItemCard';
import { items } from '@/gamedata/items';

const templates: readonly ItemTemplate[] = items;

export function templateOf(id: string): ItemTemplate | undefined {
  return templates.find((t) => t.id === id);
}

export function itemName(template: string): string {
  return templateOf(template)?.name ?? template;
}

/** An icon until there is item art: the name's initials. */
export function initials(template: string): string {
  const words = itemName(template).split(' ');
  return words.length > 1 ? `${words[0]![0]}${words[1]![0]}` : itemName(template).slice(0, 2);
}

/** 1 when the template is unknown. */
export function starsOf(template: string): number {
  return templateOf(template)?.stars ?? 1;
}

/** The class that gives an icon's frame or an item's name its star colour. */
export function starClass(template: string): string {
  return `stars-${starsOf(template)}`;
}

export function starMarks(template: string): string {
  return '★'.repeat(starsOf(template));
}
