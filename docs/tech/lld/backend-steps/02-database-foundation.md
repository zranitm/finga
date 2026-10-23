# 02. Database foundation

- **Depends on:** 01
- **Milestone:** Foundations
- **Order:** 2
- **Status:** Done
- **Step:** 02. Database foundation

**Goal:** talk to Postgres safely, migrate it, and test against a real one.

## Scope

- `pkg/dbx`: a connection pool from config; `DBTX`, which the pool and a transaction both satisfy; and `InTx(ctx, db, fn)`: READ COMMITTED, retried up to 3 times with jitter on a deadlock (`40P01`) or serialization failure (`40001`). Only module stores call it. Services depend on their module's `Store`, so their rule tests use a fake store.
- `migrations/embed.go` and the first migration, which enables `citext`.
- `cmd/migrate`: applies pending migrations with goose, then exits.
- `sqlc.yaml`, so each module generates its own query code; `make gen` runs it.
- `pkg/pgtest`: a test helper that starts Postgres with Testcontainers once per test run, applies the migrations to a template database, and gives each test a fresh copy of it. Imported by tests only.

## Design notes

- Each module's store wraps `dbx.InTx` in its own `InTx(ctx, fn func(uow UnitOfWork) error)`: it hands `fn` a unit of work whose repositories are bound to the transaction, then classifies the error. Repository methods never take a transaction, and services never see one. One use case can still span several repositories, or another module's rules through the unit of work, such as a new character and its starter items (page 1, Rules at the boundaries). Step 05 builds the first store.
- depguard allows `pgx` and `pkg/dbx` only in `repository.go` files, `internal/server`, and tests, so a service can't reach for a transaction.
- The function passed to `InTx` may run more than once, so it must have no side effects outside the transaction. The godoc says so.

## Tests first

- `InTx` commits on success, rolls back on an error, and rolls back then re-panics on a panic.
- Errors from `fn` and from commit stay reachable with `errors.As`, so a store's `classify` finds the `PgError` underneath.
- It retries on `40001` and `40P01` up to the limit, then returns the error. It doesn't retry other errors, and stops when the context is cancelled.
- Migrations apply to an empty database, and a second run changes nothing.
- `pkg/pgtest` gives each test an isolated database: two tests writing the same row don't see each other.

## Done when

- [ ] `go run ./cmd/migrate` works against the Compose Postgres.
- [ ] Integration tests run in CI on GitHub's runners.

## Review focus

- Retries wrap the whole transaction function, never a single statement.
