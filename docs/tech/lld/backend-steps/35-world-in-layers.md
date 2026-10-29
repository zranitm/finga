# 35. World in layers

- **Depends on:** 34
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 35
- **Status:** Done
- **Step:** 35. World in layers

**Goal:** the world service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the world service's.

## Scope

- `internal/world` and `internal/combat` become `model` (what a joining player and a loaded character carry, the views and delivered events, the saves, pickups and their outcomes, the combat rules, the errors) and `application` (the World, the map instance's actor and tick with its working state, the economy and persistence workers, and the ports for the character, item and social services and for delivery). `clients` and `handler` implement those ports. No repository. The instance's private player, mob and drop state stays with the actor in `application` (decision 264).
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the world service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [x] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=world` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
