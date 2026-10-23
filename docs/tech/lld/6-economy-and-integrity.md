# 6. Economy and integrity

## Ledger accounts (Stage 2)

- `wallet:<character id>` for each character.
- `system:mint`: allowed to go negative; its balance is minus the total coins ever created.
- `system:sink:market_fee`: collects marketplace fees.

Because coins only ever move between accounts, the sum of all balances is always zero.

## Transfers (Stage 2)

| Kind | Postings |
| --- | --- |
| `loot_coins` | mint −n, wallet +n |
| `market_purchase` | buyer −price, seller +(price − fee), fee sink +fee |

Fee = floor(price × fee_bps / 10,000), with fee_bps 500 (5%, OD-10 default), stored on the listing at listing time.

## Transaction rules

- Every economic command is one `InTx` call on its module's store (`dbx.InTx` underneath): READ COMMITTED isolation with explicit row locks, retried up to 3 times on a deadlock or serialization error.
- A market command's unit of work carries the item rules and the ledger, bound to the same transaction (page 1, Rules at the boundaries). The market service never sees the transaction.
- **Lock order, always:** listing, then items, then ledger accounts. `pkg/ledger` locks its accounts in ID order itself. A fixed order prevents deadlocks.
- The idempotency check runs first, inside the same transaction, so a command and its stored response commit or roll back together.

## Market commands (Stage 2)

**List**

1. Idempotency check.
2. Lock the item: it must belong to the seller and be in the inventory.
3. For a partial stack, split it: a new item holds the listed quantity, with item events on both.
4. Set the item's location to listed and insert the listing (expires in 7 days, OD-10 default).
5. Outbox event ItemListed. Commit.

**Buy**

1. Idempotency check.
2. Lock the listing. It must be active and unexpired, and the buyer can't be the seller; otherwise listing_unavailable.
3. Lock the item and find a slot in the buyer's inventory (a stack with room, or a free slot). None: inventory_full (TD-03).
4. Post `market_purchase`. A balance that would go negative fails with insufficient_balance.
5. Move the item to the buyer's slot, with item event sold.
6. Set the listing to sold, with buyer and close time.
7. Outbox event TradeSettled. Commit.
8. After commit, push WalletChanged and InventoryChanged to the buyer, and WalletChanged to the seller if online.

**Cancel and reclaim:** cancelling an active listing, or reclaiming an expired one, needs a free inventory slot. The item returns to the inventory and the listing becomes cancelled or reclaimed.

**Expiry job:** every minute it locks up to 100 active, past-expiry listings with `FOR UPDATE SKIP LOCKED` and marks them expired. The item stays out of the inventory until the seller reclaims it, so expiry never needs free space.

## Concurrency guarantees

- **Two buyers, one listing:** the listing's row lock serializes them; the second sees sold and gets listing_unavailable.
- **Retried command:** the same idempotency key returns the stored response with no second effect. The same key with a different request returns idempotency_key_reused.
- **Double pickup:** the map instance's reservation stops it, and the drop ID as idempotency key makes a retried job harmless.

## Events

- Written to the outbox in the same transaction as the change: CoinsLooted, ItemLooted, ItemListed, ListingCancelled, ListingExpired, ListingReclaimed, TradeSettled, ItemDiscarded, CharacterLeveledUp.
- One topic per domain (TD-04): economy, item, market, character. Keyed by the aggregate ID.
- Every event has a unique event ID for deduplication by consumers (Stage 2).

## Invariant checks

Each check returns a list of mismatches; any mismatch is a failure.

- **Stage 1:** the checks are plain functions in `internal/application/item/invariants.go` (`CheckInvariants` and one function per check), run by the property, race, retry, and crash tests; the crash test lives in `test/`, which is why they are exported rather than test helpers. There is no auditor binary.
- **Stage 2:** they move into `internal/application/audit`, and the auditor runs them continuously as a Kafka consumer.

1. All ledger balances sum to zero.
2. Each account's balance equals the sum of its postings.
3. Each transfer's postings sum to zero.
4. No wallet balance is negative.
5. Each item's quantity equals the sum of its item events' quantity deltas.
6. Per item template, total quantity equals what drops created minus what was discarded.
7. Every listed item has exactly one open listing (active or expired), and every open listing points to a listed item.
8. No two items share an inventory slot.
9. Every sold listing has exactly one `market_purchase` transfer and one TradeSettled event.

Stage 1's tests run checks 5, 6, and 8 (items). Checks 1 to 4 (the ledger) and 7 and 9 (the market) join in Stage 2.

## Tests

- **Property-based** (`pgregory.net/rapid`): random sequences of loots, inventory moves, and discards (plus listings, purchases, cancels, and reclaims from Stage 2) against a real Postgres, after which every invariant check must pass.
- **Race:** 50 goroutines pick up the same drop; exactly one gets it. From Stage 2, also 50 buyers for one listing; exactly one succeeds.
- **Retry:** the same command sent twice returns the same response and changes nothing the second time.
- **Crash:** kill the server in the middle of these runs, restart, and every invariant check must still pass.
