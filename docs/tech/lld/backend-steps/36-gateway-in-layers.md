# 36. Gateway in layers

- **Depends on:** 35
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 36
- **Status:** Done
- **Step:** 36. Gateway in layers

**Goal:** the gateway service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the gateway service's.

## Scope

- The client protocol is the model's vocabulary (the user's choice; decision 267). `model` gets a failure as a client sees it (`Public`, `ServiceError`, `PublicOf`), the message envelope and close codes, a connection's player, presence and rate limits, the REST API's accounts and characters, and the services' stream events; `application` the Lobby (REST) and the Gateway (Hello, join and rejoin, the rate limits and command queue, routing, kicks, the drain) with ports for the five services and the connection; `clients` implements the ports over gRPC, turns deliveries into protocol messages and wraps every failure (`errmap`, moved from the handler, with `Wrap`); `handler/api` and `handler/ws` adapt HTTP and the socket. No repository.
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the gateway service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [x] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=gateway` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
