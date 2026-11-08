// What the inventory and the character window both say about an item.
import { items as templates } from '@/gamedata/items';

export type Template = (typeof templates)[number];

export function templateOf(id: string): Template | undefined {
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

/** The class that frames an icon in its star colour; 1 when the template is unknown. */
export function starClass(template: string): string {
  return `stars-${templateOf(template)?.stars ?? 1}`;
}
