# C11. Drops, pickup and the inventory window

- **Depends on:** C10
- **Features:** D1, D2, D5
- **Milestone:** M3. Loot and keep
- **Order:** 11
- **Status:** Done
- **Step:** C11. Drops, pickup and the inventory window

**Goal:** loot appears, one key picks it up, and the inventory window shows and moves what you hold.

**Layer in this step:** `game/entities/drop`, `game/systems/pickup`, `features/inventory` (page 7 layer 4).

## Scope

- Drop entities from Snapshot and Delta with the item sprite and a bounce on appearance; removed on `removed_drops`; a pickup-range hint when the player's box centre is within 40 px.
- Pick up: the key sends `PickUp` with the current sequence number; the inventory updates only from `InventoryChanged`. A failed pickup comes back as a Response with no request ID (`inventory_full`, or `unavailable` when the server is busy), shown as a toast through net's `onUnmatchedResponse` (C04). An out-of-range pickup gets no answer, since the server ignores it; that is what the range hint is for.
- Inventory window (I key): three tabs of 24 slots from the `inventory` store; drag to move within a tab sends `InventoryMove` with a request ID and one idempotency key per drag. The item stays in its slot, marked pending, until the Response, and only `InventoryChanged` moves it. On a timeout the window offers Retry, which re-sends with the same key; across a reconnect, net re-sends it by itself (C04). Discard sends `InventoryDiscard` the same way, after a confirm. Cross-tab drags are refused locally, matching the server's rule. Item names and icons come from the generated item templates (C03).
- Starter items from Welcome appear on first open.

## Design notes

- D5 on the client means: one idempotency key per user action, kept until a Response arrives, reused on retry, never regenerated. The key lives in the pending-request map in `net`, not in React state.

## Tests first

- pickup system: the key within range sends one PickUp; pressing it twice in one tick sends one; out of range sends nothing and shows the hint off
- a Response with no request ID and `inventory_full` shows the toast
- inventory (Testing Library): a drag between slots sends InventoryMove with the item, tab and slot; a timed-out request retried sends the same idempotency key; a refused Response clears the pending mark and shows the reason, the item never having moved; a cross-tab drag never sends
- store and window agree after a sequence of InventoryChanged payloads from the golden file shapes
- discard asks first, then sends InventoryDiscard with the whole stack and one key; a drop on the item's own slot sends nothing
- the toasts store keeps the newest four; a toast goes on a click or after four seconds; wiring maps `inventory_full`, `unavailable` and other refusals to words

## Done when

- [ ] Against `make up`: kill a slime, pick up the drop once, see it in the window, move it, discard one, and reconnect to find the inventory unchanged. (Not run: no browser is attached to the implementation session; owed to the author with the M1 checks.)
- [ ] M3 milestone: with two browsers, both see the drop and only one gets it when both press pick up.

## Review focus

- No optimistic inventory update that the server could contradict without a visible correction.
