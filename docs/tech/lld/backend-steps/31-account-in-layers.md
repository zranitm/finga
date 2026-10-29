# 31. Account in layers

- **Depends on:** 30
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 31
- **Status:** Ready
- **Step:** 31. Account in layers

**Goal:** the account service in the layers of page 11, with its behaviour unchanged.

**Layers in this step:** all of the account service's.

## Scope

- `internal/account` splits into `model` (Account, Session, Settings, the errors), `application` (register, login, sessions, settings; `ports.go`) and `repository` (the accounts and sessions tables, `queries/`, `internal/db/`). The handler imports `application` and `model`.
- Tests move with the code they test: rule tests with fakes to `application`, Postgres tests to `repository`, the error table's tests stay in `handler`.
- Page 10's tree and the README's architecture section show the account service's layers.

## Tests first

- none new: the step moves code, and the tests it moves must pass unchanged in what they assert; the layer rules of step 30 hold for the service

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; `make test-service SERVICE=account` green; CI green.

## Review focus

- Imports point inward: nothing in `model` or `application` knows gRPC, Postgres or the configuration.
- No behaviour change hidden in the move.
