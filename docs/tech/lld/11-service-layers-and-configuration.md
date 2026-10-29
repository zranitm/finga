# 11. Service layers and configuration

On 2026-10-29 the user asked for two changes to the services of page 10:

- **Each service follows clean architecture, with its layers as packages.** Until now a service kept its layers as files of one package (`internal/item/` held `api.go`, `deps.go`, `service.go` and `repository.go`), so the tree did not show them. The user chose the names: `model`, `application`, `repository`, with `handler` and `clients` at the edges.
- **Each service has its own configuration.** Until now one file, `server/configs/config.yaml`, held every key of every binary, and each binary loaded all of it. Now each service owns a file with only its keys, inside the service.

This page is the plan: the layers and what each may import, one service's tree, how today's files move, the configuration per service, and the steps that build it (backend steps 29 to 36). Pages 1, 3 and 10 are updated by each step as the code changes, so they always say what the code does; this page says where the steps are going. Behaviour does not change: no contract, message, table or rule changes in these steps.

## The layers

Dependencies point inward, toward `model`. Outer layers implement what inner layers declare.

| Package | Holds | May import | Never imports |
| --- | --- | --- | --- |
| `internal/model` | The service's entities and value types, its domain errors (sentinels), its domain events, and rules that need nothing but data | the shared kernel, `pkg/` helpers with no I/O, the standard library | every other layer of the service, `config`, gRPC, `api/gen`, pgx, `pkg/dbx`, `protocol`, `telemetry` |
| `internal/application` | The use cases (`Service`): rules that need state, who may do what, where a transaction starts, idempotency, outbox events. The ports it needs, declared where they are used: its `Store` and `UnitOfWork`, repositories, the other services it calls, observers | `model`, the shared kernel, `pkg/` | `repository`, `handler`, `clients`, `config`, gRPC, `api/gen`, pgx, `protocol`, `telemetry` |
| `internal/repository` | The Postgres store that implements the application's `Store`: `InTx`, the unit of work, `classify`, the queries (`queries/`) and sqlc's output (`internal/db/`, private to it) | `application` (the ports it implements), `model`, `pkg/dbx`, pgx | `handler`, `clients`, gRPC, `api/gen`, `protocol` |
| `internal/handler` | The gRPC server: proto in, one use case, proto or a status with a reason out; the error table from `model`'s errors to the contract's reasons | `application`, `model`, `api/gen`, `pkg/grpcx` | `repository`, `clients`, pgx, `pkg/dbx` |
| `internal/clients` | gRPC adapters that implement the application's ports for the services this one calls | `application`, `model`, `api/gen`, `pkg/grpcx` | `repository`, `handler`, pgx |
| `config` | The service's configuration file (`config.yaml`) and the type that checks it. Outside `internal/`, so the end-to-end tests can build each service with its own (decision 259) | `internal/host`, `pkg/dbx` | everything else |
| `cmd/<name>/app` | The composition root: reads the configuration, builds each layer, wires them | everything in the service | |

The rules are checked by depguard (`.golangci.yml`), one rule per layer, as page 1's import direction is today.

A service without a part has no package for it: `social` and `world` keep nothing in Postgres, so they have no `repository` (social's parties stay in memory in its application, TD-19); `account`, `item` and `social` call no service, so they have no `clients`.

### The shared kernel's model

The root `internal/model` (Finga's ID types and `Class`) clashes with the name every service's `model` now takes, so it becomes `internal/kernel`: `kernel.CharacterID`, `kernel.ClassWarrior`. It stays part of the shared kernel with `internal/gamedata`, `internal/sim` and `internal/progression`.

## One service

```txt
services/item/
  cmd/item/main.go          reads the configuration, builds the service, runs it until SIGINT or SIGTERM
  cmd/item/app/             the composition root
  config/                   config.yaml, the item service's defaults with only its keys, and Config, the type that checks them
  migrations/               its goose files, embedded; its own version table
  invariants/               the item invariant checks (unchanged; the tests and a Stage 2 auditor run them)
  internal/
    model/                  Item, Event, Cause, Slot, the errors (ErrInventoryFull, ...), the outbox payloads
    application/            Service (Add, Move, Discard, Grant, Inventory), its inputs and results, ports.go
    repository/             Postgres, the unit of work, classify; queries/ and internal/db/
    handler/                the gRPC server and its error table
```

How today's files move, in every service with a module:

| Today (`internal/<module>/`) | After |
| --- | --- |
| `api.go`: entities, errors, constants | `model/` |
| `api.go`: a use case's inputs and results (`AddInput`, `Result`) | `application/` |
| `events.go`: domain events and their payloads | `model/events.go` |
| `deps.go`: `Store`, `UnitOfWork`, repositories, other modules | `application/ports.go` |
| `service.go` | `application/service.go` (a service with two modules keeps one file per module: `application/party.go`, `application/chat.go`) |
| `repository.go`, `queries/`, `internal/db/` | `repository/` (`sqlc.yaml` points at the new folders) |
| `internal/handler/`, `internal/clients/` | unchanged, importing `application` and `model` |

Tests move with the code they test: rule tests with fakes go to `application`, the repository tests against Postgres to `repository`, the error tables' completeness tests stay in `handler`.

### Per service

- **account:** `model` holds Account, Session, Settings and the errors; `application` the register, login, session and settings use cases; `repository` the accounts and sessions tables.
- **character:** `model` holds Character, its stats and the errors; `application` create, list, checkpoint and level-up, with the item service behind a port (`StarterItems`) that `clients` implements; `repository` the characters table.
- **item:** as the tree above. The invariant checks stay a public package of the service.
- **social:** `model` holds Party, Invite, the chat line and the errors; `application` the party and chat use cases with their in-memory state, and the delivery port the handler's streams implement. No repository, no clients.
- **world:** `model` holds the world's entities (the player, mob and drop state a map instance keeps), the combat rules (today's `internal/combat`), the drop and portal rules that need nothing but data, the delivered events and the errors; `application` the World (join, leave, inputs), the map instance's actor and tick, the economy and persistence workers, and the ports for the character, item and social services and for delivery, which `clients` and `handler` implement. No repository.
- **gateway:** `model` holds the connection's session state, the limits and the delivered events in the gateway's own types; `application` the connection flows: Hello, join and rejoin on a world restart, the command queue and routing, kicks and the drain, with ports for the five services. `handler/api` (REST), `handler/ws` (the socket: decode, rate limit, encode) and `handler/errmap` turn the client protocol into use cases and back; `clients` implements the ports over gRPC. No repository.

## Configuration per service

Each service reads one file, its own: `services/<name>/config/config.yaml`, with only the keys it uses. The type and its checks live beside it, in the same package (`services/<name>/config`). `cmd/migrate` has its own file the same way (`cmd/migrate/config/config.yaml`: the database and the log level). The root `server/configs/config.yaml` and `internal/config` are removed.

```yaml
# services/world/config/config.yaml
grpc:
  listen: ":9004"
ops:
  addr: ":9104"            # /healthz, /readyz, /metrics
shutdown_grace: 10s
log_level: info
data_dir: data
tick_rate_hz: 20
checkpoint_interval: 30s   # TD-02
economy:
  workers: 8
dependencies:              # where this service dials the ones it calls
  character: localhost:9002
  item: localhost:9003
  social: localhost:9005
```

| Service | Keys besides `grpc`, `ops`, `shutdown_grace`, `log_level` |
| --- | --- |
| `gateway` | `http.addr` (:8080) and `http.cors_origins` instead of `grpc`; `dependencies` on all five |
| `account` | `database` |
| `character` | `database`, `data_dir`, `dependencies.item` |
| `item` | `database`, `data_dir` |
| `world` | `data_dir`, `tick_rate_hz`, `checkpoint_interval`, `economy.workers`, `dependencies` on character, item and social |
| `social` | none |
| `migrate` | `database` and `log_level` only |

- **Overrides keep their form:** `FINGA_` and the key's path in upper case, read by the one binary that has the key: `FINGA_DATABASE_URL`, `FINGA_DEPENDENCIES_ITEM`, `FINGA_HTTP_ADDR` (was `FINGA_SERVER_ADDR`). Compose sets each service's own.
- **Paths stay relative to `server/`,** as `data/` is: a binary run from `server/` (`make run`, the process tests) reads `services/<name>/config/config.yaml`, and the image puts the file at the same path under `/app`. `CONFIG_PATH` still names any file, and a git-ignored `config.local.yaml` next to the defaults still replaces them.
- **Shared parts are types, not a shared file:** `internal/host` has the types of the sections every service has (`GRPCSection`, `OpsSection`), and `pkg/dbx` the database section's (`dbx.Config`); each service's `Config` lists its keys with them and hands the host its part (`Host()`), and `host.Main` loads the service's own file for it.

## Steps

| Step | What | Commit type |
| --- | --- | --- |
| 29 | Configuration per service: every binary's own file and type in `config/`; the root file and `internal/config` removed; Compose, `make run`, the Dockerfile and the README follow | `refactor(config)` |
| 30 | The shared kernel's `internal/model` becomes `internal/kernel`; the depguard rules for the layers | `refactor` |
| 31 | account in layers | `refactor(account)` |
| 32 | item in layers | `refactor(item)` |
| 33 | character in layers | `refactor(character)` |
| 34 | social in layers | `refactor(social)` |
| 35 | world in layers | `refactor(world)` |
| 36 | gateway in layers | `refactor(gateway)` |

Each step keeps every test green and moves the tests with the code; a step adds tests only where it adds code (the configuration loaders, the layer rules). `make test`, `make lint`, `make cover`, `make up` and `make smoke` pass after each, one commit each, pushed with CI green.
