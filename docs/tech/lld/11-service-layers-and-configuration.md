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
| `config` | The service's configuration file (`config.yaml`) and the type that checks it. Outside `internal/`, so the end-to-end tests can build each service with its own | `internal/host`, `pkg/dbx`, `pkg/logger` | everything else |
| `app` | The composition root (with `main.go` beside it at the service's root): reads the configuration, builds each layer, wires them | everything in the service | |

The rules are checked by depguard (`.golangci.yml`), one rule per layer, as page 1's import direction is today. One exception, in the rules: a handler's tests may wire the real application and clients over fake services, as the composition root does. The gateway's `model` and `application` import no contract either, the client protocol (`api/gen/client/v1`, generated from `api/proto/client/v1`, TD-27) included, and no `net/http`.

A service without a part has no package for it: `world` keeps no state of its own, so it has no `repository`; `account`, `item` and `social` call no service, so they have no `clients`. `social` keeps its parties behind a store port with an in-memory repository (TD-19), so moving them to Redis or Postgres is a new repository, not a change to the party rules.

### The shared kernel's model

The root `internal/model` (Finga's ID types and `Class`) clashes with the name every service's `model` now takes, so it becomes `internal/kernel`: `kernel.CharacterID`, `kernel.ClassWarrior`. It stays part of the shared kernel with `internal/gamedata`, `internal/sim` and `internal/progression`.

## One service

```txt
services/item/
  main.go                   reads the configuration, builds the service, runs it until SIGINT or SIGTERM
  app/                      the composition root
  config/                   config.yaml, the item service's defaults with only its keys, and Config, the type that checks them
  migrations/               its goose files, embedded; its own version table
  itemtest/                 test support: the item invariant checks, which only tests run
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

- **account:** `model` holds Account, Session, Settings and the errors; `application` the register, login, session and settings use cases, and the notifier port that hears of ended sessions; `repository` the accounts and sessions tables.
- **character:** `model` holds Character, its stats and the errors; `application` create, list, checkpoint and level-up, with the item service behind a port (`StarterItems`) that `clients` implements; `repository` the characters table.
- **item:** as the tree above. The invariant checks are a test-support package of the service (`itemtest`, like `pkg/pgtest`); the Stage 2 auditor gets its own (page 6).
- **social:** `model` holds Party, Invite, the chat line and the errors; `application` the party and chat use cases, the `PartyStore` port (a unit of work, `InTx`, as account's and item's stores are) and the notifier port; `repository` the in-memory store, which stages a transaction's writes and applies them only if it succeeds. No clients.
- **world:** `model` holds what a joining player and a loaded character carry, the views and the events delivered each tick, the checkpoints it saves, pickups and their outcomes, the combat rules (today's `internal/combat`) and the errors; `application` the World (join, leave, inputs), the map instance's actor with its working state (players, mobs, drops and spawns while it ticks), the economy and persistence workers, and the ports for the character, item and social services and for delivery, which `clients` and `handler` implement. No repository.
- **gateway:** `model` has its own views of what a player is shown (a character, an item, a party, the players, mobs and drops on a map), and the client protocol (`api/gen/client/v1`) is the WebSocket handler's alone: it decodes the client's messages and turns the views into its own, as the REST handler does requests (the user's calls, 2026-11-01 and 2026-11-04; until then the protocol's views were the model's vocabulary). `model` also holds a failure as a client sees it (`Public`: a code and the player's message, every one the gateway or a service raises declared in `model/failures.go`; `ServiceError`; the gateway's own errors), the commands' inputs (`Keys`, `ItemMove`, `ItemDiscard`), `Welcome`, a connection's player and presence, why a session ends (`EndReason`), the services' pushes (`Delivery`, one type per push, built from the model's views) and stream events, and the REST API's accounts and characters; `application` holds the Lobby (the REST API's use cases) and the Gateway as two flows: `application/send` (what a connection asks for: Hello, the commands, the close) and `application/push` (what the services' streams and the gateway's timers start: routing pushes, ended sessions, the drain), over `application/session` (the `Session`, the `Registry`, the joins and rejoin, and the player's `Outbox`: Welcome, Deliver, and End with a reason; the gateway knows a connection only by its ID). The two flows never import each other, and each declares the ports it uses, so `send.World` is the world's commands and `push.World` its delivery stream; `clients` implements the ports over gRPC, turns the services' stream events into pushes, and wraps every failure through `errmap`, which maps each service reason to one of the model's failures; `handler/api` (REST on Gin, TD-26) and `handler/ws` (WebSocket on `pkg/wsconn`: `ws/receive` routes what the client sends on `pkg/wsrouter`, `ws/push` writes what it is sent) adapt the transports; `handler/api` also gives each failure its HTTP status, and `testdata/errors/codes.json` is written from that table. Each owns its conversation: routes, the request's or message's decoding and validation, the reply, and on the WebSocket each push's message; the WebSocket handler also the handshake, the per-message rate limits and each connection's command queue, which only a long-lived connection has, and what an end looks like to the client (Kicked and 4003 where the protocol has a reason, else 1011 and the client reconnects). No repository. The application imports neither gRPC nor `net/http` nor `pkg/wsconn` nor `pkg/wsrouter`, and builds no client message.

A service whose use cases tell connected players of a change (account: ended sessions; social: party state and invites) takes its notifier port as a constructor argument, and the handler's `Streams` (the gateways' subscriptions on `pkg/hub`) implement it. `Streams` is built first and handed to both the service and the gRPC server, so neither waits for the other.

## Configuration per service

Each service reads one file, its own: `services/<name>/config/config.yaml`, with only the keys it uses. The type and its checks live beside it, in the same package (`services/<name>/config`). `cmd/migrate` has its own file the same way (`cmd/migrate/config/config.yaml`: the database and the log section). The root `server/configs/config.yaml` and `internal/config` are removed.

```yaml
# services/world/config/config.yaml
grpc:
  listen: ":9004"
ops:
  addr: ":9104"            # /healthz, /readyz, /metrics
shutdown_grace: 10s
log:
  level: info
  format: json             # or console: coloured lines for a terminal
  source: false            # add each call's file:line
  sampling:                # each message at each level per second: the first 100, then every 100th
    initial: 100
    thereafter: 100
data_dir: data
tick_rate_hz: 20           # divides 60; sent to the client in Welcome
checkpoint_interval: 30s   # TD-02
flush_timeout: 10s         # the last saves on shutdown, all together; after shutdown_grace
economy:
  workers: 8
dependencies:              # where this service dials the ones it calls
  character: localhost:9002
  item: localhost:9003
  social: localhost:9005
```

| Service | Keys besides `grpc`, `ops`, `shutdown_grace`, `log` |
| --- | --- |
| `gateway` | `http.addr` (:8080) and `http.cors_origins` instead of `grpc`, and `ops.addr` (:9100); `ws.hello_timeout` (5s) and `ws.command_queue` (64); `sessions.join_timeout` (5s) and `sessions.check_interval` (1m); `dependencies` on all five |
| `account` | `database`, `password` (the Argon2id cost: `time` 1, `memory_kib` 65536, `threads` 4; and `concurrent` 4 hashes at once, a login past them waiting up to `wait` 1s, then answered `rate_limited`) |
| `character` | `database`, `data_dir`, `dependencies.item` |
| `item` | `database`, `data_dir` |
| `world` | `data_dir`, `tick_rate_hz`, `checkpoint_interval`, `flush_timeout`, `economy.workers`, `dependencies` on character, item and social |
| `social` | none |
| `migrate` | `database` and `log` only |

- **Overrides keep their form:** `FINGA_` and the key's path in upper case, read by the one binary that has the key: `FINGA_DATABASE_URL`, `FINGA_DEPENDENCIES_ITEM`, `FINGA_HTTP_ADDR` (was `FINGA_SERVER_ADDR`). Compose sets each service's own.
- **Paths stay relative to `server/`,** as `data/` is: a binary run from `server/` (`make run`, the process tests) reads `services/<name>/config/config.yaml`, and the image puts the file at the same path under `/app`. `CONFIG_PATH` still names any file, and a git-ignored `config.local.yaml` next to the defaults still replaces them.
- **Database timeouts:** each pooled connection gets `statement_timeout` (5s), `lock_timeout` (2s) and `idle_in_transaction_timeout` (10s) from the `database` section, so a stuck lock or a forgotten transaction cannot park the workers that hold a connection; `migrate` sets only `lock_timeout` (10s), since a migration may run long but must not queue every query behind a lock. A zero leaves Postgres's default, which is none.
- **Shared parts are types, not a shared file:** `internal/host` has the types of the sections every service has (`GRPCSection`, `OpsSection`), `pkg/dbx` the database section's (`dbx.Config`), and `pkg/logger` the log section's (`logger.Config`, TD-28); each service's `Config` lists its keys with them and hands the host its part (`Host()`), and `host.Main` loads the service's own file for it.

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
