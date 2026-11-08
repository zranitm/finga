// Only InventoryChanged moves an item; a timed-out move retries with the same idempotency key.
import { useState } from 'react';
import type { DragEvent } from 'react';

import { Button } from '@/components/Button';
import { Window } from '@/components/Window';
import { carried, carry } from '@/features/shared/drag';
import { initials, itemName, starClass } from '@/features/shared/items';
import { useItemRequests } from '@/features/shared/useItemRequests';
import type { GameSocket } from '@/net/socket';
import type { ItemView } from '@/protocol/messages';
import { SLOTS_PER_TAB, inventory, tabs } from '@/state/inventory';
import type { Tab } from '@/state/inventory';

export interface InventoryWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

const tabLabels: Readonly<Record<Tab, string>> = { equip: 'Equip', use: 'Use', etc: 'Etc' };

export function InventoryWindow({ socket, onClose }: InventoryWindowProps) {
  const slots = inventory.use((s) => s.slots);
  const [tab, setTab] = useState<Tab>('equip');
  const [selected, setSelected] = useState<string | null>(null);
  const [dragging, setDragging] = useState<ItemView | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState<ItemView | null>(null);
  const { pending, notice, setNotice, run, retry } = useItemRequests();

  const move = (item: ItemView, toTab: Tab, slot: number) => {
    if (toTab !== item.tab) {
      setNotice('Items stay in their tab.');
      return;
    }
    if (slot === item.slot || pending.has(item.itemId)) {
      return;
    }
    run(item.itemId, (key) =>
      socket.request('inventoryMove', {
        idempotencyKey: key,
        itemId: item.itemId,
        tab: toTab,
        slot,
      }),
    );
  };

  const discard = (item: ItemView) => {
    setConfirmDiscard(null);
    setSelected(null);
    run(item.itemId, (key) =>
      socket.request('inventoryDiscard', {
        idempotencyKey: key,
        itemId: item.itemId,
        quantity: item.quantity,
      }),
    );
  };

  /** Wears an Equip-tab item; the server picks its slot, swapping out what is there. */
  const equip = (item: ItemView) => {
    if (item.tab !== 'equip' || pending.has(item.itemId)) {
      return;
    }
    run(item.itemId, (key) =>
      socket.request('equip', { idempotencyKey: key, itemId: item.itemId }),
    );
  };

  /** Takes off what an equip slot holds, into slot when it is in the Equip tab, else the first free one. */
  const unequip = (equipSlot: string, slot: number | undefined) => {
    if (pending.has(equipSlot)) {
      return;
    }
    run(equipSlot, (key) =>
      socket.request('unequip', {
        idempotencyKey: key,
        equipSlot,
        ...(slot === undefined ? {} : { toSlot: slot }),
      }),
    );
  };

  const onDrop = (e: DragEvent, slot: number) => {
    e.preventDefault();
    if (dragging) {
      move(dragging, tab, slot);
      setDragging(null);
      return;
    }
    const d = carried(e);
    if (d?.from === 'worn') {
      unequip(d.equipSlot, tab === 'equip' ? slot : undefined);
    }
  };

  const selectedItem = selected ? (slots[tab].find((it) => it?.itemId === selected) ?? null) : null;
  const selectedPending = selected ? pending.get(selected) : undefined;

  return (
    <Window title="Inventory" onClose={onClose} initialX={410} initialY={80}>
      <div className="inventory">
        <div className="inventory-tabs" role="tablist">
          {tabs.map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={t === tab}
              className={`btn btn-quiet${t === tab ? ' tab-active' : ''}`}
              onClick={() => {
                setTab(t);
                setSelected(null);
              }}
            >
              {tabLabels[t]}
            </button>
          ))}
        </div>
        <div className="inventory-grid" role="grid" aria-label={`${tabLabels[tab]} items`}>
          {Array.from({ length: SLOTS_PER_TAB }, (_, slot) => {
            const item = slots[tab][slot] ?? null;
            const isPending = item ? pending.has(item.itemId) : false;
            return (
              <button
                key={slot}
                type="button"
                role="gridcell"
                aria-label={
                  item ? `${itemName(item.template)} x${item.quantity}` : `empty slot ${slot + 1}`
                }
                aria-busy={isPending}
                className={`slot${item ? ` slot-${item.tab} ${starClass(item.template)}` : ''}${isPending ? ' slot-pending' : ''}${
                  item?.itemId === selected ? ' slot-selected' : ''
                }`}
                draggable={item !== null && !isPending}
                onDragStart={(e) => {
                  setDragging(item);
                  if (item) {
                    carry(e, { from: 'inventory', itemId: item.itemId });
                  }
                }}
                onDragEnd={() => setDragging(null)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => onDrop(e, slot)}
                onClick={() => setSelected(item ? item.itemId : null)}
                onDoubleClick={() => {
                  if (item) {
                    equip(item);
                  }
                }}
              >
                {item && (
                  <>
                    <span className="slot-icon" aria-hidden="true">
                      {initials(item.template)}
                    </span>
                    {item.quantity > 1 && <span className="slot-qty">{item.quantity}</span>}
                  </>
                )}
              </button>
            );
          })}
        </div>
        {selectedItem && (
          <div className="inventory-detail">
            <span>
              {itemName(selectedItem.template)}
              {selectedItem.quantity > 1 ? ` x${selectedItem.quantity}` : ''}
            </span>
            {selectedPending?.retryable ? (
              <Button variant="quiet" onClick={() => retry(selectedItem.itemId)}>
                Retry
              </Button>
            ) : (
              <Button
                variant="danger"
                disabled={selectedPending !== undefined}
                onClick={() => setConfirmDiscard(selectedItem)}
              >
                Discard
              </Button>
            )}
          </div>
        )}
        {confirmDiscard && (
          <div className="inventory-confirm" role="alertdialog" aria-label="Discard">
            <span>Discard {itemName(confirmDiscard.template)}?</span>
            <Button variant="danger" onClick={() => discard(confirmDiscard)}>
              Yes, discard
            </Button>
            <Button variant="quiet" onClick={() => setConfirmDiscard(null)}>
              Keep
            </Button>
          </div>
        )}
        {(notice ?? selectedPending?.error) && (
          <p className="error" role="alert">
            {notice ?? selectedPending?.error}
          </p>
        )}
      </div>
    </Window>
  );
}
