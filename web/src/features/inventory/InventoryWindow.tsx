// Only InventoryChanged moves an item; a timed-out move retries with the same idempotency key.
import { useState } from 'react';
import type { DragEvent } from 'react';

import { Button } from '@/components/Button';
import { carried, carry } from '@/features/shared/drag';
import { ItemDetails, useItemHover } from '@/features/shared/ItemHover';
import { initials, itemName, starClass, starMarks, starsOf } from '@/features/shared/items';
import { useItemRequests } from '@/features/shared/useItemRequests';
import { WorldWindow } from '@/features/shared/WorldWindow';
import type { GameSocket } from '@/net/socket';
import { healOf, missing, restores } from '@/progression/potions';
import { ERR, errorText } from '@/protocol/errors';
import type { ItemView } from '@/protocol/messages';
import { character } from '@/state/character';
import { combatLog } from '@/state/combat-log';
import { SLOTS_PER_TAB, inventory, tabs } from '@/state/inventory';
import type { Tab } from '@/state/inventory';

export interface InventoryWindowProps {
  readonly socket: Pick<GameSocket, 'request'>;
  readonly onClose: () => void;
}

const tabLabels: Readonly<Record<Tab, string>> = { equip: 'Equip', use: 'Use', etc: 'Etc' };

const DISCARD_MANY = 'discard-many'; // the one batch discard's pending request
const NAMED_STARS = 4; // the batch confirm names every ticked item of this many stars or more

const listed = new Intl.ListFormat('en', { type: 'conjunction' });

/** The batch confirm: how many items, naming the rare ones so a misclick on one shows. */
function discardQuestion(items: readonly ItemView[]): string {
  const rare = new Map<string, number>();
  for (const it of items) {
    if (starsOf(it.template) >= NAMED_STARS) {
      rare.set(it.template, (rare.get(it.template) ?? 0) + 1);
    }
  }
  const names = [...rare].map(([t, n]) => `${itemName(t)} ${starMarks(t)}${n > 1 ? ` x${n}` : ''}`);
  const count = `${items.length} ${items.length === 1 ? 'item' : 'items'}`;
  return names.length === 0
    ? `Discard ${count}?`
    : `Discard ${count}, including ${listed.format(names)}?`;
}

export function InventoryWindow({ socket, onClose }: InventoryWindowProps) {
  const slots = inventory.use((s) => s.slots);
  const [tab, setTab] = useState<Tab>('equip');
  const [selected, setSelected] = useState<string | null>(null);
  const [dragging, setDragging] = useState<ItemView | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState<ItemView | null>(null);
  const [ticking, setTicking] = useState(false);
  const [ticked, setTicked] = useState<ReadonlySet<string>>(new Set());
  const [confirmMany, setConfirmMany] = useState(false);
  const { pending, notice, setNotice, run, retry } = useItemRequests('inventory');
  const hover = useItemHover<{ tab: Tab; slot: number }>();

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

  const batch = pending.get(DISCARD_MANY);
  const tickedItems = slots[tab].filter((it): it is ItemView => !!it && ticked.has(it.itemId));

  const untick = () => {
    setTicked(new Set());
    setConfirmMany(false);
  };

  const leaveTicking = () => {
    setTicking(false);
    untick();
  };

  /** Ticks or unticks an item, unless it or a batch is on its way. */
  const toggle = (item: ItemView) => {
    if (batch || pending.has(item.itemId)) {
      return;
    }
    setConfirmMany(false);
    setTicked((prev) => {
      const next = new Set(prev);
      if (!next.delete(item.itemId)) {
        next.add(item.itemId);
      }
      return next;
    });
  };

  /** Discards the ticked items whole in one request, all of them or none; once done, select mode ends. */
  const discardMany = (items: readonly ItemView[]) => {
    setConfirmMany(false);
    const itemIds = items.map((it) => it.itemId);
    run(DISCARD_MANY, (key) =>
      socket.request('inventoryDiscardMany', { idempotencyKey: key, itemIds }).then(leaveTicking),
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

  /** Drinks a potion; one with nothing to restore is refused here as the world would refuse it. */
  const drink = (item: ItemView) => {
    const c = character.getState().character;
    if (!c || !healOf(item.template) || pending.has(item.itemId)) {
      return;
    }
    if (missing(c) === 0) {
      setNotice(errorText(ERR.HP_FULL));
      return;
    }
    const restored = restores(item.template, c);
    run(item.itemId, (key) =>
      socket
        .request('useItem', { idempotencyKey: key, itemId: item.itemId, template: item.template })
        .then(() => combatLog.getState().drank(item.template, restored)),
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
  const shownPending = selectedPending ?? (ticking ? batch : undefined);

  return (
    <WorldWindow
      name="inventory"
      title="Inventory"
      onClose={onClose}
      initialX={610}
      initialY={80}
      footer={
        (notice ?? shownPending?.error) && (
          <p className="error" role="alert">
            {notice ?? shownPending?.error}
          </p>
        )
      }
    >
      <div className="inventory">
        <div className="inventory-head">
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
                  untick();
                }}
              >
                {tabLabels[t]}
              </button>
            ))}
          </div>
          {ticking ? (
            <Button variant="quiet" onClick={leaveTicking}>
              Cancel
            </Button>
          ) : (
            <Button
              variant="quiet"
              onClick={() => {
                setTicking(true);
                setSelected(null);
                setConfirmDiscard(null);
              }}
            >
              Select
            </Button>
          )}
        </div>
        <div className="inventory-grid" role="grid" aria-label={`${tabLabels[tab]} items`}>
          {Array.from({ length: SLOTS_PER_TAB }, (_, slot) => {
            const item = slots[tab][slot] ?? null;
            const isTicked = item !== null && ticked.has(item.itemId);
            const isPending = item
              ? pending.has(item.itemId) || (isTicked && batch !== undefined && !batch.retryable)
              : false;
            return (
              <button
                key={slot}
                type="button"
                role="gridcell"
                aria-label={
                  item ? `${itemName(item.template)} x${item.quantity}` : `empty slot ${slot + 1}`
                }
                aria-busy={isPending}
                aria-selected={ticking && item ? isTicked : undefined}
                className={`slot${item ? ` slot-${item.tab} ${starClass(item.template)}` : ''}${isPending ? ' slot-pending' : ''}${
                  item?.itemId === selected ? ' slot-selected' : ''
                }${isTicked ? ' slot-ticked' : ''}`}
                draggable={item !== null && !isPending && !ticking}
                {...hover.bind({ tab, slot }, item, tab === 'equip')}
                onDragStart={(e) => {
                  hover.clear();
                  setDragging(item);
                  if (item) {
                    carry(e, { from: 'inventory', itemId: item.itemId });
                  }
                }}
                onDragEnd={() => setDragging(null)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={(e) => onDrop(e, slot)}
                onClick={() => {
                  if (!ticking) {
                    setSelected(item ? item.itemId : null);
                  } else if (item) {
                    toggle(item);
                  }
                }}
                onDoubleClick={() => {
                  if (ticking) {
                    return;
                  }
                  if (item?.tab === 'equip') {
                    equip(item);
                  } else if (item?.tab === 'use') {
                    drink(item);
                  }
                }}
              >
                {item && (
                  <>
                    {ticking && (
                      <span className="slot-tick" aria-hidden="true">
                        {isTicked ? '✓' : ''}
                      </span>
                    )}
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
              <>
                {healOf(selectedItem.template) && (
                  <Button
                    disabled={selectedPending !== undefined}
                    onClick={() => drink(selectedItem)}
                  >
                    Use
                  </Button>
                )}
                <Button
                  variant="danger"
                  disabled={selectedPending !== undefined}
                  onClick={() => setConfirmDiscard(selectedItem)}
                >
                  Discard
                </Button>
              </>
            )}
          </div>
        )}
        {ticking && (
          <div className="inventory-detail">
            <span>Click items to tick them.</span>
            {batch?.retryable ? (
              <Button variant="quiet" onClick={() => retry(DISCARD_MANY)}>
                Retry
              </Button>
            ) : (
              <Button
                variant="danger"
                disabled={tickedItems.length === 0 || batch !== undefined}
                onClick={() => setConfirmMany(true)}
              >
                Discard {tickedItems.length} {tickedItems.length === 1 ? 'item' : 'items'}
              </Button>
            )}
          </div>
        )}
        {confirmMany && tickedItems.length > 0 && (
          <div className="inventory-confirm" role="alertdialog" aria-label="Discard items">
            <span>{discardQuestion(tickedItems)}</span>
            <Button variant="danger" onClick={() => discardMany(tickedItems)}>
              Yes, discard
            </Button>
            <Button variant="quiet" onClick={() => setConfirmMany(false)}>
              Keep
            </Button>
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
      </div>
      <ItemDetails
        hovered={hover.hovered}
        item={hover.hovered ? (slots[hover.hovered.key.tab][hover.hovered.key.slot] ?? null) : null}
      />
    </WorldWindow>
  );
}
