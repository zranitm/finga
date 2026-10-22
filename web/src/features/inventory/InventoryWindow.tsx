// The inventory window (C11): three tabs of 24 slots from the inventory
// store. A drag within a tab sends InventoryMove with one idempotency key
// per drag; the item stays in its slot, marked pending, until the
// Response, and only InventoryChanged moves it. A timed-out request offers
// Retry with the same key. Discard asks first. Cross-tab drags are refused
// here, matching the server's rule.
import { useState } from 'react';
import type { DragEvent } from 'react';

import { Button } from '@/components/Button';
import { Window } from '@/components/Window';
import { items as templates } from '@/gamedata/items';
import { idempotencyKey } from '@/lib/id';
import { RequestError } from '@/net/socket';
import type { GameSocket } from '@/net/socket';
import type { ItemView, Tab } from '@/protocol/messages';
import { SLOTS_PER_TAB, inventory, tabs } from '@/state/inventory';

export interface InventoryWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
  readonly onFocusChange?: (focused: boolean) => void;
}

const tabLabels: Readonly<Record<Tab, string>> = { equip: 'Equip', use: 'Use', etc: 'Etc' };

/** The template's display name, or the id when the data has none. */
export function itemName(template: string): string {
  return templates.find((t) => t.id === template)?.name ?? template;
}

/** Two letters for the slot tile, from the template name. */
function initials(template: string): string {
  const words = itemName(template).split(' ');
  return words.length > 1 ? `${words[0]![0]}${words[1]![0]}` : itemName(template).slice(0, 2);
}

interface PendingRequest {
  readonly key: string;
  readonly send: () => Promise<unknown>;
  readonly retryable: boolean; // a timeout or a lost connection: the same key again
  readonly error: string | null;
}

export function InventoryWindow({ socket, onClose, onFocusChange }: InventoryWindowProps) {
  const slots = inventory.use((s) => s.slots);
  const [tab, setTab] = useState<Tab>('equip');
  const [selected, setSelected] = useState<string | null>(null);
  const [dragging, setDragging] = useState<ItemView | null>(null);
  const [pending, setPending] = useState<ReadonlyMap<string, PendingRequest>>(new Map());
  const [confirmDiscard, setConfirmDiscard] = useState<ItemView | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const setPendingFor = (itemId: string, p: PendingRequest | null) => {
    setPending((prev) => {
      const next = new Map(prev);
      if (p) {
        next.set(itemId, p);
      } else {
        next.delete(itemId);
      }
      return next;
    });
  };

  /** Runs a request for an item: pending until the Response, retryable on a timeout. */
  const run = async (itemId: string, key: string, send: () => Promise<unknown>) => {
    setPendingFor(itemId, { key, send, retryable: false, error: null });
    setNotice(null);
    try {
      await send();
      setPendingFor(itemId, null); // InventoryChanged does the moving
    } catch (err) {
      if (err instanceof RequestError && (err.code === 'timeout' || err.code === 'disconnected')) {
        setPendingFor(itemId, { key, send, retryable: true, error: 'No answer yet.' });
      } else {
        setPendingFor(itemId, null);
        setNotice(err instanceof Error ? err.message : 'The server refused.');
      }
    }
  };

  const move = (item: ItemView, toTab: Tab, slot: number) => {
    if (toTab !== item.tab) {
      setNotice('Items stay in their tab.');
      return;
    }
    if (slot === item.slot || pending.has(item.item_id)) {
      return;
    }
    const key = idempotencyKey();
    void run(item.item_id, key, () =>
      socket.request('InventoryMove', {
        idempotency_key: key,
        item_id: item.item_id,
        tab: toTab,
        slot,
      }),
    );
  };

  const discard = (item: ItemView) => {
    setConfirmDiscard(null);
    setSelected(null);
    const key = idempotencyKey();
    void run(item.item_id, key, () =>
      socket.request('InventoryDiscard', {
        idempotency_key: key,
        item_id: item.item_id,
        quantity: item.quantity,
      }),
    );
  };

  const retry = (itemId: string) => {
    const p = pending.get(itemId);
    if (p?.retryable) {
      void run(itemId, p.key, p.send);
    }
  };

  const onDrop = (e: DragEvent, slot: number) => {
    e.preventDefault();
    if (dragging) {
      move(dragging, tab, slot);
      setDragging(null);
    }
  };

  const selectedItem = selected
    ? (slots[tab].find((it) => it?.item_id === selected) ?? null)
    : null;
  const selectedPending = selected ? pending.get(selected) : undefined;

  return (
    <Window
      title="Inventory"
      onClose={onClose}
      {...(onFocusChange && { onFocusChange })}
      initialX={320}
      initialY={80}
    >
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
            const isPending = item ? pending.has(item.item_id) : false;
            return (
              <button
                key={slot}
                type="button"
                role="gridcell"
                aria-label={
                  item ? `${itemName(item.template)} x${item.quantity}` : `empty slot ${slot + 1}`
                }
                aria-busy={isPending}
                className={`slot${item ? ` slot-${item.tab}` : ''}${isPending ? ' slot-pending' : ''}${
                  item?.item_id === selected ? ' slot-selected' : ''
                }`}
                draggable={item !== null && !isPending}
                onDragStart={() => setDragging(item)}
                onDragEnd={() => setDragging(null)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => onDrop(e, slot)}
                onClick={() => setSelected(item ? item.item_id : null)}
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
              <Button variant="quiet" onClick={() => retry(selectedItem.item_id)}>
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
