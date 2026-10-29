# 36. Gateway in layers

- **Depends on:** 35
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 36
- **Status:** Ready
- **Step:** 36. Gateway in layers

**Goal:** the gateway service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the gateway service's.

## Scope

- `model` gets the connection's session state, the limits and the delivered events in the gateway's own types; `application` the connection flows (Hello, join and rejoin, the command queue and routing, kicks, the drain) with ports for the five services; `handler/api`, `handler/ws` and `handler/errmap` keep the client protocol at the edge (decode, rate limit, encode); `clients` implements the ports over gRPC, turning proto into the gateway's types. No repository.
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the gateway service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=gateway` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
