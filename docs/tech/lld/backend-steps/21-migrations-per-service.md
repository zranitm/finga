# 21. Migrations per service

- **Depends on:** 20
- **Milestone:** Services
- **Order:** 21
- **Status:** Done
- **Step:** 21. Migrations per service

**Goal:** each service with tables owns its migrations and its goose version table, so a service's schema can change on its own and no query can read another service's tables (page 10, Migrations).

**Layers in this step:** repository and the migrate binary. A refactor: the tables do not change.

## Scope

- `services/account/migrations`: `citext`, `accounts`, `sessions`, the rotation columns; version table `goose_account_version`.
- `services/character/migrations`: `citext`, `characters`; `goose_character_version`.
- `services/item/migrations`: `outbox`, `idempotency_keys`, `items`, `item_events`; `goose_item_version`. Only the item module uses the outbox and the idempotency keys, so the item service owns them.
- `citext` is created by account and character with `IF NOT EXISTS`, and neither drops it on the way down.
- Each set is a small package (`FS`, `Apply`, `Up`) over `pkg/dbx.Migrate(ctx, pool, fsys, table)`, which runs goose with the given version table.
- `cmd/migrate` applies account, character and item in that order. A database migrated before this step (it has `goose_db_version`) is refused with a message saying to recreate it (`make down`); Stage 1 has no data worth carrying over.
- `server/migrations/` goes.
- Each module's tests apply only their own service's migrations; the end-to-end tests apply all three.
- `sqlc.yaml`: each module's schema is its own service's migrations, so each generated `models.go` holds only that service's tables.

## Design notes

- The services live in `services/<name>/` from this step on, even before their code moves there: a `migrations` package is not under `internal/`, so the game server and the tests can import it.
- Page 2 (conventions, table ownership) is updated in this step.

## Tests first

- every service's set applies to an empty database, and a second run applies nothing (table)
- each set records its versions in its own table, and goose's default table does not exist
- a database that has `goose_db_version` is refused, the message names `make down`, and nothing is applied
- item's migrations alone create no `accounts` table, and account's alone create no `items` table
- every file in every set is numbered, SQL, and has an Up and a Down section (the existing check, per set)

## Done when

- [ ] `make test`, `make lint`, `make cover` green; `make gen` regenerates the same queries with smaller `models.go` files; `make up` from an empty volume works.

## Review focus

- The legacy guard: no half-applied state when it refuses.
- `citext` survives rolling back either service.
