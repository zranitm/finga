# TD-22. One Postgres schema or one per module?

- **Area:** Data
- **Decision:** TD-22. One Postgres schema or one per module?
- **Default:** One schema
- **Outcome:** One schema (public) with plain table names, so queries stay short. Module ownership is kept by convention and listed in the data model's Table ownership table.
- **Status:** Decided

## Options considered

- **One schema per module** (`account.accounts`, `market.listings`): ownership shows in every query, can be enforced later with per-module database roles, and a module's tables move to a new database with one `pg_dump -n`.
- **One schema:** shorter queries and migrations, less setup.

## Why one schema

Queries stay short, and for a solo project the enforcement option isn't worth the extra length in every query.

## What still holds

- Each module owns its tables, listed in Low-level design, page 2.
- No foreign keys across modules, so a module's tables can still move to their own database in Stage 2.
- Generic packages keep a configurable table prefix (`ledger_` for `pkg/ledger`).
