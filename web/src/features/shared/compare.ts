// What a hovered item is compared with, and how: the rules the item card colours.
import { itemStats } from '@/components/item-stats';
import type { StatKey } from '@/components/item-stats';
import type { ItemTemplate, Unmet } from '@/components/ItemCard';
import { equipSlots } from '@/gamedata/equip-slots';
import { statsShort } from '@/progression/stats';
import type { CharacterView, ItemStats } from '@/protocol/messages';
import type { EquipSlotId, Equipment } from '@/state/inventory';

/** The equip slot a double-click fills for an item of this kind: its first empty slot, else its first, as the server picks; null for a kind no slot takes. */
export function slotFor(kind: string, equipment: Equipment): EquipSlotId | null {
  const slots = equipSlots.filter((s) => s.kind === kind);
  return (slots.find((s) => equipment[s.id] === null) ?? slots[0])?.id ?? null;
}

/** Per stat, what wearing the hovered roll instead of the worn one changes; a stat only one has counts in full, and with nothing worn every stat is a gain. */
export function changes(
  hovered: ItemStats | undefined,
  worn: ItemStats | undefined,
): Partial<Record<StatKey, number>> {
  const out: Partial<Record<StatKey, number>> = {};
  for (const { key } of itemStats) {
    const by = (hovered?.[key] ?? 0) - (worn?.[key] ?? 0);
    if (by !== 0) {
      out[key] = by;
    }
  }
  return out;
}

/** The requirements the character misses, stats against its own; nothing without a character. */
export function unmet(t: ItemTemplate, c: CharacterView | null): Unmet {
  if (c === null) {
    return { level: false, class: false, stats: [] };
  }
  return {
    level: c.level < (t.min_level ?? 0),
    class: t.class !== undefined && t.class !== c.class,
    stats: c.stats ? statsShort(t, c.stats) : [],
  };
}
