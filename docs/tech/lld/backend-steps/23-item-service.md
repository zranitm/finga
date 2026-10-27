# 23. Item service

- **Depends on:** 22
- **Features:** D2, D5
- **Milestone:** Services
- **Order:** 23
- **Status:** Done
- **Step:** 23. Item service

**Goal:** items run as their own service. Pickups, inventory commands, Welcome's inventory and starter items reach it over gRPC, and no transaction spans two services.

**Layers in this step:** the item module (moved), its gRPC handler, its binary; adapters in the game server's composition root.

## Scope

**Item service**

- `internal/application/item` moves to `services/item/internal/item` as it is.
- `GrantStarter(ctx, actor, class)` becomes the module's own command: one transaction, idempotent under scope `starter` with the character ID as the key, so a second call changes nothing. `Within` and `Scoped` go; page 1 keeps the pattern for modules inside one service.
- `services/item/internal/handler`: `ItemService` over the module, with the error table (every item error to a code and an `item.v1` reason).
- `services/item/cmd/item` and `app`: configuration, the pool, game data, the module, the gRPC and ops listeners on `pkg/runner`.
- `invariants.go` moves to `services/item/invariants`, a package outside `internal/`, so the end-to-end tests (and later the auditor) can run the checks.

**Callers, still in the game server**

- Character creation commits the character, then calls `GrantStarter` with retries and backoff until it succeeds or the request's deadline ends; it answers success once the character is committed. `StarterItems` leaves the character's unit of work and becomes a dependency of the service.
- The world's pickups call `Add` through an adapter; a failure with no reason is retried once, as `item.IsRule` decided before.
- The gateway's Welcome inventory and the two inventory commands call the item service.
- `errmap` maps the item reasons to the same codes; `PickupFailed` carries the remote error.

**Packaging**

- Compose runs the item service; the game server dials it. The end-to-end stack builds it in the test process. The crash test runs the item service and the game server as processes and kills both.

## Design notes

- The world's join grants the starter items as a safety net from step 27, when the world loads the character itself. Until then, a crash between the commit and the grant leaves a character without its starter items.

## Tests first

- item: `GrantStarter` twice for one character grants once and the second call returns the same slots; for an unknown class it is `unknown_item` and writes nothing
- handler, over `bufconn` with the real module on Postgres: each RPC maps its fields and its result (table); every item error arrives with its reason (table); the guard: every exported `Err` in the module has a row
- character: creation commits and then grants; a grant that fails twice then succeeds is retried; a grant that never succeeds before the deadline still returns the character
- world adapter: `Add` with a reason is not retried, one without is retried once
- end-to-end rows unchanged (D1, D2, D5, the double and retried pickups, the WebSocket re-send); the crash test with two processes keeps the invariants

## Done when

- [ ] `make up` runs Postgres, migrate, item and the game server; `make smoke` passes; `make test`, `make lint`, `make cover` green.

## Review focus

- No item rule left in the game server: it only forwards.
- The idempotency key of each call: the drop ID for pickups, the client's key for inventory commands, the character ID for starter items.
