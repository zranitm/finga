# 01. Foundations: config, errors, logging, shared types

- **Depends on:** 00
- **Milestone:** Foundations
- **Order:** 1
- **Status:** Done
- **Step:** 01. Foundations: config, errors, logging, shared types

> **Changed by step 20 (2026-10-27):** the ID types in `internal/model` sit on `pkg/typedid`, with the same names and behaviour.

**Goal:** the small pieces every later package uses.

## Scope

- `internal/config`: the schema from page 1 (server, database, world, economy, telemetry), loaded through `pkg/configload`, with a `Validate` method for rules tags can't express.
- `pkg/errs`: the error codes from page 4 (`invalid_argument`, `unauthenticated`, `not_found`, `conflict`, `rate_limited`, `unavailable`, `internal`), as a typed error that carries a code and a message safe to show players.
- `pkg/observability`: JSON `slog` logger with a configurable level, a Prometheus registry, and the `/metrics` handler (TD-21 default).
- `internal/telemetry`: the metrics named in page 3, defined and registered. Later steps add the observer interfaces each application module declares, and `telemetry` implements them.
- `internal/model`: separate ID types (`AccountID`, `CharacterID`, `ItemID`, `SessionID`) over UUIDv7, and the `Class` type with parsing.

## Design notes

- Each module declares its errors in `api.go` as `errs` values with domain codes (`inventory_full`, `idempotency_key_reused`). Services return them for rule violations, and the module's `classify` turns database errors into them. Handlers map codes without knowing the domain (page 1, Layers and responsibilities).
- Separate ID types make it a compile error to pass a character ID where an item ID is expected.

## Tests first

- **config:** the committed defaults load; every validation rule rejects its bad value, naming the key; `FINGA_WORLD_TICK_RATE_HZ` overrides the YAML.
- **errs:** wrapping keeps the code; `errors.As` finds it through wrapping; the player message never includes the wrapped error's text.
- **model:** each class parses and prints back; an unknown class is rejected.
- **observability:** records below the level are dropped; `/metrics` serves a registered metric.

## Done when

- [ ] Tests pass and coverage is at least 80%.

## Review focus

- No global logger (sloglint enforces it).
- Error messages never echo user input.
