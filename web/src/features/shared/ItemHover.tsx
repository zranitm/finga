// The card beside the pointer, shared by the inventory and the character window.
import { useState } from 'react';
import type { MouseEvent } from 'react';

import { ItemCard, NothingWornCard } from '@/components/ItemCard';
import type { Point } from '@/lib/placement';
import type { ItemView } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';

import { BesidePointer } from './BesidePointer';
import { changes, slotFor, unmet } from './compare';
import { templateOf } from './items';

/** A hovered slot; its card shows what the slot holds now, so an item that leaves takes its card along. */
export interface Hovered<K> {
  readonly key: K;
  /** Also shows what is worn in the slot this item would take. */
  readonly compare: boolean;
  readonly at: Point;
}

export interface ItemHover<K> {
  readonly hovered: Hovered<K> | null;
  /** The mouse handlers for a slot's element; an empty one only lets go. */
  readonly bind: (
    key: K,
    item: ItemView | null,
    compare: boolean,
  ) => {
    onMouseEnter?: (e: MouseEvent) => void;
    onMouseMove?: (e: MouseEvent) => void;
    onMouseLeave: () => void;
  };
  /** Hides the card, as a drag starts. */
  readonly clear: () => void;
}

export function useItemHover<K>(): ItemHover<K> {
  const [hovered, setHovered] = useState<Hovered<K> | null>(null);
  const leave = () => setHovered(null);
  return {
    hovered,
    bind: (key, item, compare) =>
      item
        ? {
            onMouseEnter: (e) => setHovered({ key, compare, at: { x: e.clientX, y: e.clientY } }),
            onMouseMove: (e) =>
              setHovered((h) => h && { ...h, at: { x: e.clientX, y: e.clientY } }),
            onMouseLeave: leave,
          }
        : { onMouseLeave: leave },
    clear: leave,
  };
}

/** The card of item, which the hovered slot holds now, with the worn item's beside it when it compares, kept fully on screen. */
export function ItemDetails<K>({
  hovered,
  item,
}: {
  readonly hovered: Hovered<K> | null;
  readonly item: ItemView | null;
}) {
  const equipment = inventory.use((s) => s.equipment);
  const c = character.use((s) => s.character);
  const t = item ? templateOf(item.template) : undefined;
  if (!hovered || !item || !t) {
    return null;
  }
  const slot = hovered.compare && t.slot !== undefined ? slotFor(t.slot, equipment) : null;
  const worn = slot ? equipment[slot] : null;
  const wornTemplate = worn ? templateOf(worn.template) : undefined;
  return (
    <BesidePointer at={hovered.at}>
      <ItemCard
        template={t}
        view={item}
        unmet={unmet(t, c)}
        {...(slot ? { changes: changes(item.stats, worn?.stats) } : {})}
      />
      {slot &&
        (worn && wornTemplate ? (
          <ItemCard heading="Worn" template={wornTemplate} view={worn} />
        ) : (
          <NothingWornCard />
        ))}
    </BesidePointer>
  );
}
