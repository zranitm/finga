# 13. Drops and pickup

- **Depends on:** 11, 12
- **Features:** D1, D2, D5
- **Milestone:** M3. Loot and keep
- **Order:** 13
- **Status:** Done
- **Step:** 13. Drops and pickup

**Goal:** mobs drop items, players pick them up, and races, retries, and crashes never create or lose an item. This completes M3, Loot and keep.

## Scope

- In `internal/application/world`:
  - Drops rolled from each mob's drop table (items only in Stage 1).
  - Ground drops in memory: they expire after 60 seconds, and for the first 10 seconds only the killer can take them (the party joins in step 15).
  - PickUp within 40 px: the instance reserves the drop and hands a job to the economy worker pool.
- The worker pool (8 workers): calls the item service's add with the drop ID as the idempotency key, then reports the result to the instance's inbox. Success removes the drop and sends InventoryChanged; failure releases the reservation.
- InventoryMove and InventoryDiscard over WebSocket: gateway handlers that decode the message, call the item service with the connection's character, and map the result to a Response, as in page 1's example.

## Tests first

- drop rolls follow the table with a fixed generator
- two players pick up the same drop in the same tick: exactly one gets it
- a retried pickup job for the same drop gives one item
- a full inventory leaves the drop on the ground and releases the reservation
- the loot window blocks another player for 10 seconds, then allows them
- drops vanish after 60 seconds
- crash test: run `cmd/gameserver` as a separate process, drive concurrent pickups with test clients, kill it with SIGKILL, restart it, and the invariant checks pass
- end-to-end rows for D1, D2, and D5, including Stage 1's exit criterion: a double pickup and a retried pickup each give exactly one item

## Done when

- [ ] The race, retry, and crash tests pass.

## Review focus

- The tick never waits on the database.
- The reservation is released on every failure path.
