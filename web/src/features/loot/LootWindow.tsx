// The loot window: every item in the player's pouches within reach, as one list; a click takes a row, and Take all or the loot key takes them all.
import { create } from '@bufbuild/protobuf';
import { useMemo } from 'react';

import { Button } from '@/components/Button';
import { ItemDetails, useItemHover } from '@/features/shared/ItemHover';
import { initials, itemName, starClass, starMarks, templateOf } from '@/features/shared/items';
import { useNow } from '@/features/shared/useNow';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { ItemViewSchema } from '@/protocol/messages';
import type { ItemView, PouchItem } from '@/protocol/messages';
import { pickKey, pouches } from '@/state/pouches';
import type { ItemPick } from '@/state/pouches';

export interface LootWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

interface Row {
  readonly key: string;
  readonly pick: ItemPick;
  readonly view: ItemView;
}

/** An item in a pouch as the item card shows it. */
const viewOf = (it: PouchItem): ItemView =>
  create(ItemViewSchema, {
    template: it.template,
    tab: templateOf(it.template)?.tab ?? '',
    quantity: it.quantity,
    ...(it.stats && { stats: it.stats }),
  });

export function LootWindow({ socket, onClose }: LootWindowProps) {
  const all = pouches.use((s) => s.pouches);
  const inReach = pouches.use((s) => s.inReach);
  const taking = pouches.use((s) => s.taking);
  const refused = pouches.use((s) => s.refused);
  const hover = useItemHover<string>();
  const now = useNow(1000);
  const shown = useMemo(() => all.filter((p) => inReach.includes(p.id)), [all, inReach]);
  const rows = useMemo(
    () =>
      shown.flatMap((p) =>
        p.items.map((it): Row => {
          const pick = { pouch: p.id, item: it.item };
          return { key: pickKey(pick), pick, view: viewOf(it) };
        }),
      ),
    [shown],
  );
  // The store asks only for picks not already being taken.
  const take = (picks: readonly ItemPick[]) => {
    void pouches
      .getState()
      .take(picks, (asked) => socket.request('takeLoot', { picks: [...asked] }));
  };
  const allTaking = rows.every((r) => taking.has(r.key));
  const goneIn = Math.max(0, Math.ceil((Math.min(...shown.map((p) => p.goneAt)) - now) / 1000));
  return (
    <WorldWindow
      name="loot"
      title="Loot"
      onClose={onClose}
      initialX={780}
      initialY={260}
      footer={
        <div className="loot-foot">
          {shown.length > 0 && <span className="muted">Gone in {goneIn} s</span>}
          <Button disabled={allTaking} onClick={() => take(rows.map((r) => r.pick))}>
            Take all
          </Button>
        </div>
      }
    >
      <ul className="loot-rows" aria-label="Items in reach">
        {rows.map((r) => {
          const pending = taking.has(r.key);
          const why = refused.get(r.key);
          const t = r.view.template;
          return (
            <li key={r.key} className={starClass(t)}>
              <button
                type="button"
                className={`loot-row slot-${r.view.tab}${pending ? ' slot-pending' : ''}`}
                disabled={pending}
                aria-busy={pending}
                onClick={() => take([r.pick])}
                {...hover.bind(r.key, r.view, true)}
              >
                <span className="slot-icon" aria-hidden="true">
                  {initials(t)}
                </span>
                <span className="loot-name">
                  {itemName(t)} {starMarks(t)}
                </span>
                {r.view.quantity > 1 && <span className="loot-qty">×{r.view.quantity}</span>}
              </button>
              {why && (
                <p className="loot-refused" role="alert">
                  {why}
                </p>
              )}
            </li>
          );
        })}
      </ul>
      <ItemDetails
        hovered={hover.hovered}
        item={rows.find((r) => r.key === hover.hovered?.key)?.view ?? null}
      />
    </WorldWindow>
  );
}
