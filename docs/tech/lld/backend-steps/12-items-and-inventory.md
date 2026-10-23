# 12. Items and inventory

- **Depends on:** 02, 04, 06
- **Features:** A2, D2, D5
- **Milestone:** M3. Loot and keep
- **Order:** 12
- **Status:** Done
- **Step:** 12. Items and inventory

**Goal:** items that are never duplicated or lost, with a full history, and inventory actions that are safe to retry.

**Layers in this step:** repository and service, plus two shared libraries. The WebSocket commands are handled in step 13.

## Scope

**Shared libraries**

- `pkg/idempotency`: `Store.Do` keyed by owner, scope, and key, storing a hash of the request. The store is built over a connection (`NewStore(tx)`), so a unit of work binds it to its transaction. The same key with a different request gives `idempotency_key_reused`.
- `pkg/outbox`: `Writer.Add`, on a writer built over a connection (`NewWriter(tx)`) like the idempotency store; JSON payloads for now.
- Migrations for `outbox` and `idempotency_keys` (page 2).

**Repository** (`internal/application/item/repository.go`)

- Migration for `items` and `item_events`, and their sqlc queries.
- Lock an owned item (`LockOwned`); lock a character's inventory rows in ID order; list occupied slots per tab; list stacks of an item with their quantities; insert an item; change quantity and location; append an item event.
- The store (`Postgres`): `InTx` with a unit of work over the item and event repositories, the outbox, and idempotency; plain reads on the pool; and `classify`: no row becomes `ErrNotFound`, and `one_item_per_slot` becomes `ErrSlotTaken`. `UnitOfWork(tx)` builds the same unit of work over another module's transaction.

**Service** (`internal/application/item/service.go`)

- The inventory rules from page 5: equip, use, and etc tabs, 24 slots each, stacks of up to 100.
- Add to inventory: the first stack with room, then the first free slot, otherwise `inventory_full` with nothing written.
- Move to an empty slot, or merge into a stack of the same item. Discard a whole item or part of a stack.
- Every change in one `InTx`, all through the unit of work: the idempotency check first, then the item rows, the item events, and an outbox event.
- Grant starter items inside character creation's transaction: `Within(uow).GrantStarter` runs the item rules on a unit of work over that transaction and classifies their errors. The character module declares `StarterItems` and exposes it as `Starter()` on its unit of work. `internal/server` binds it with `items.Within(itemStore.UnitOfWork(tx))`, so neither module imports the other.
- The invariant checks 5, 6, and 8 as test helpers in `internal/application/item/invariants_test.go`.

## Design notes

- Moving onto a different item is a `conflict` in Stage 1, not a swap. Simpler, and enough for the MVP.
- The service decides ownership, and the repository makes it atomic by putting the owner in the `WHERE` clause (page 1).
- Slot counts and stack sizes are rules, so they live in the service. The repository only reports what's occupied.

## Tests first

**Repository** (real Postgres, through the store)

- `LockOwned` on someone else's item gives `ErrNotFound`
- occupied slots and stack quantities, including for a full inventory
- two items in one slot are rejected and come back as `ErrSlotTaken`
- `classify`, as a table test: no row, our constraints, another module's constraint left untouched, and `40001` passed through for the retry

**Service** (real Postgres, since these rules are about transactions)

- add: into an existing stack; overflow into a new slot; a full inventory gives `inventory_full` and writes nothing
- move: to an empty slot; merging into a matching stack; onto a different item gives `conflict`
- discard: part of a stack, and a whole item
- someone else's item gives `not_found`
- idempotency: the same key twice gives the same response and one effect; the same key with a different request gives `idempotency_key_reused`
- a new character has its starter items, in the same transaction as the character; a failed starter item rolls the character back
- outbox rows exist exactly when their change commits, and none after a rollback
- property test (`rapid`): random sequences of adds, moves, and discards always leave the invariants true

## Done when

- [ ] Tests pass, including the property test.

## Review focus

- Rows are locked in ID order, so concurrent actions can't deadlock.
- Idempotency keys are scoped to the account.
- No inventory rule in the repository.
