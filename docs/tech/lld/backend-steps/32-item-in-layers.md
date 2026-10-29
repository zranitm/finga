# 32. Item in layers

- **Depends on:** 31
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 32
- **Status:** Ready
- **Step:** 32. Item in layers

**Goal:** the item service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the item service's.

## Scope

- `internal/item` splits into `model` (Item, Event, Cause, Slot, the errors, the outbox payloads), `application` (add, move, discard, grant, inventory; their inputs and results; `ports.go`) and `repository` (the items and item events tables, the idempotency and outbox writers it binds, `queries/`, `internal/db/`). `invariants/` stays a public package of the service.
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the item service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=item` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
