# TD-25. Split into a gateway and services in Stage 1?

- **Area:** Architecture
- **Decision:** TD-25. Split into a gateway and services in Stage 1?
- **Default:** Split now: one node per service in Stage 1
- **Outcome:** Split now, decided by the user on 2026-10-27. The game runs as six processes behind a connection gateway (gateway, account, character, item, world, social), one repository and one Go module, gRPC between them, one node each in Stage 1. Stage 2 adds more nodes, a registry, and NATS.
- **Status:** Decided

## Options

- **A. Keep one binary for Stage 1** and split at HLD section 15's step 3, when a metric shows it is needed.
- **B. Split now,** with one node per service, so Stage 1 is the base for scaling later.

## Why B

- **Deploys.** A game service restarts without dropping a socket: the gateway keeps the WebSocket open and rejoins its players after a world restart, so only the gateway needs a rolling drain.
- The boundaries already exist: each module owns its tables, modules talk through small interfaces, and the tick never waits on the database. Splitting along them is a layout change, not a rewrite.

## Consequences

- No transaction spans two services: character creation commits, then grants the starter items through the item service with the character ID as idempotency key.
- The tick reads a local copy of party membership, fed by a stream from the social service.
- Each service owns its migrations and version table; `cmd/migrate` applies them all.
- More processes to run locally; Compose runs them all, and the end-to-end tests build them in one process.

## Details

Low-level design, page 10: Gateway and services; backend steps 20 to 28.
