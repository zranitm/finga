// The card beside the pointer, shared by the inventory and the character window.
import { useLayoutEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { createPortal } from 'react-dom';

import { ItemCard, NothingWornCard } from '@/components/ItemCard';
import { besidePointer } from '@/lib/placement';
import type { Point, Size } from '@/lib/placement';
import type { ItemView } from '@/protocol/messages';
import { character } from '@/state/character';
import { inventory } from '@/state/inventory';

import { changes, slotFor, unmet } from './compare';
import { templateOf } from './items';

export interface Hovered {
  readonly item: ItemView;
  /** Also shows what is worn in the slot this item would take. */
  readonly compare: boolean;
  readonly at: Point;
}

export interface ItemHover {
  readonly hovered: Hovered | null;
  /** The mouse handlers for an item's element; none for an empty slot. */
  readonly bind: (
    item: ItemView | null,
    compare: boolean,
  ) => {
    onMouseEnter?: (e: MouseEvent) => void;
    onMouseMove?: (e: MouseEvent) => void;
    onMouseLeave?: () => void;
  };
  /** Hides the card, as a drag starts. */
  readonly clear: () => void;
}

export function useItemHover(): ItemHover {
  const [hovered, setHovered] = useState<Hovered | null>(null);
  return {
    hovered,
    bind: (item, compare) =>
      item
        ? {
            onMouseEnter: (e) => setHovered({ item, compare, at: { x: e.clientX, y: e.clientY } }),
            onMouseMove: (e) =>
              setHovered((h) => h && { ...h, at: { x: e.clientX, y: e.clientY } }),
            onMouseLeave: () => setHovered(null),
          }
        : {},
    clear: () => setHovered(null),
  };
}

/** The hovered item's card, with the worn item's beside it when it compares, kept fully on screen. */
export function ItemDetails({ hovered }: { readonly hovered: Hovered | null }) {
  const equipment = inventory.use((s) => s.equipment);
  const c = character.use((s) => s.character);
  const box = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState<Size>({ width: 0, height: 0 });

  // Measured before paint, again for each item and whatever is worn beside it, so the cards place by their real size.
  const item = hovered?.item;
  useLayoutEffect(() => {
    const r = box.current?.getBoundingClientRect();
    if (r && (r.width !== size.width || r.height !== size.height)) {
      setSize({ width: r.width, height: r.height });
    }
  }, [item, equipment, size.width, size.height]);

  const t = hovered ? templateOf(hovered.item.template) : undefined;
  if (!hovered || !t) {
    return null;
  }
  const slot = hovered.compare && t.slot !== undefined ? slotFor(t.slot, equipment) : null;
  const worn = slot ? equipment[slot] : null;
  const wornTemplate = worn ? templateOf(worn.template) : undefined;
  const at = besidePointer(hovered.at, size, {
    width: window.innerWidth,
    height: window.innerHeight,
  });
  return createPortal(
    <div ref={box} className="item-cards" style={{ left: at.x, top: at.y }}>
      <ItemCard
        template={t}
        view={hovered.item}
        unmet={unmet(t, c)}
        {...(slot ? { changes: changes(hovered.item.stats, worn?.stats) } : {})}
      />
      {slot &&
        (worn && wornTemplate ? (
          <ItemCard heading="Worn" template={wornTemplate} view={worn} />
        ) : (
          <NothingWornCard />
        ))}
    </div>,
    document.body,
  );
}
