# 33. Character in layers

- **Depends on:** 32
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 33
- **Status:** Ready
- **Step:** 33. Character in layers

**Goal:** the character service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the character service's.

## Scope

- `internal/character` splits into `model` (Character, stats, the errors), `application` (create, list, checkpoint, level-up; the starter-items port) and `repository` (the characters table). `clients` implements the starter-items port over the item service.
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the character service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=character` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
