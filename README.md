# Finga

A browser-based 2D side-scrolling MMORPG-lite: pick a class, hunt mobs, level
up, team up in parties of up to 4, and loot. Go services, Postgres, and a
Phaser client. The design lives in `docs/`: the product (what the game is) and
the tech design (how it is built), starting at `docs/README.md`.

This repository holds Stage 1, the proof of concept: the backend and the
client, built in the steps of `docs/tech/lld/8-backend-implementation-steps.md`
and `docs/tech/lld/9-client-implementation-steps.md`, one commit per step,
with the evidence trail in `docs/ai-coding-log.md` and the proof-session
checklist in `docs/proof-session.md`.

## Repository layout

```txt
server/   the Go module: the six services (services/), their tools, configuration,
          the game content (data/), and the contracts (api/, testdata/)
web/      the browser client: Phaser 4, React, Vite, TypeScript
infra/    how it runs: Docker Compose and the nginx config today; infrastructure
          as code and the Grafana dashboards join here
docs/     the design (product and tech), the AI coding log, the proof-session checklist
```

The server is the reference for everything the two sides share: the client
generates its game data from `server/data/`, copies the maps from there, and
its tests check `server/testdata/` (the message golden files, the movement
vectors, the error codes). The root `Makefile` drives all three parts.

## Build and run

```bash
make up      # everything: Postgres, the migrations, the six services and the client; the game at http://localhost:4173
make smoke   # a client signs up, enters the world and moves, against the running stack
make down    # stop everything and drop the database volume
```

`make up` is the one command: it works from a clean clone with Docker and
Compose only, the client included. The `web` image builds the client with
Node and serves it with nginx, which passes `/api` and `/ws` to the gateway,
so the browser sees one origin. Testers on the same network open
`http://<this machine's LAN address>:4173`. When another program owns a port
the stack publishes, set `FINGA_WEB_PORT` (4173), `FINGA_PORT` (8080) or
`FINGA_POSTGRES_PORT` (5432) in `infra/.env` (copy `infra/.env.example`).
Without Docker, run Postgres yourself, start the binaries, and serve the
client with the Vite dev server (see Client):

```bash
make run                   # exports server/.env, applies every service's migrations to FINGA_DATABASE_URL, starts the six services
```

The browser talks to nginx, and nginx to the gateway. The gateway serves
REST under /api, the WebSocket at /ws, /healthz, /readyz and /metrics on
:8080; every other service serves gRPC on one port and its own /healthz,
/readyz and /metrics on another (its ops port):

| Process         | gRPC | HTTP / ops                                  | On the host with `make up`                            |
|-----------------|------|---------------------------------------------|-------------------------------------------------------|
| web (nginx)     |      | 80: the client; /api and /ws to the gateway | 4173 (`FINGA_WEB_PORT`): open this one                |
| gateway         |      | 8080: /api, /ws, probes, /metrics           | 8080 (`FINGA_PORT`), for `make smoke` and `make bots` |
| account         | 9001 | 9101                                        |                                                       |
| character       | 9002 | 9102                                        |                                                       |
| item            | 9003 | 9103                                        |                                                       |
| world           | 9004 | 9104                                        | 9104 (`FINGA_WORLD_OPS_PORT`), for `make bots`        |
| social          | 9005 | 9105                                        |                                                       |
| Postgres        |      | 5432                                        | 5432 (`FINGA_POSTGRES_PORT`)                          |
| Vite dev server |      | 5173 (`npm run dev`)                        | not in Compose; for working on the client             |

Under `make up` the services reach each other on the Compose network by
name (`world:9004`), and only the ports in the last column are published;
under `make run` every port is on localhost. The defaults live in each
service's own configuration (see Configuration).

Prerequisites for working on the code: Go 1.27 or newer (a C compiler for the
race detector; on Windows, for example `winget install
BrechtSanders.WinLibs.POSIX.UCRT`), make, golangci-lint v2 for `make lint`,
sqlc for `make gen` (which also runs `buf generate` through the Go tools pinned in `server/go.mod`, so nothing else needs installing), and Docker for the tests that run a real Postgres.

## Client

The browser client lives in `web/` (Phaser 4, React 19, Vite 8, TypeScript,
Zustand, Vitest; page 7 of the design). It is built step by step after the
backend, the same way, with its own rows in `docs/ai-coding-log.md`.

`make up` already serves the client's production build on :4173. To work
on the client with hot reload, run the dev server against the running stack:

```bash
npm --prefix web install        # once; Node 24 (web/.nvmrc)
npm --prefix web run dev        # the client on :5173, /api and /ws proxied to the gateway on :8080 (make up or make run)
```

`make test`, `make lint` and `make cover` run the client's scripts too
(`npm --prefix web test`, `run lint`, `run coverage`), so one command checks
both halves. The lint step includes a fixture that must be rejected by the
layer rule (page 7's five layers, enforced with `import-x/no-restricted-paths`).

## Configuration

Each binary has its own configuration, inside it: the defaults in
`server/services/<name>/config/config.yaml` (`server/cmd/migrate/config/`
for migrate), with only the keys that binary uses, and the Go type that
checks them beside it. The binaries read their paths from where they run,
`server/`. Any key can be overridden by an environment variable named
`FINGA_` plus its path in upper case, read by each binary that has the key:
`FINGA_DATABASE_URL`, `FINGA_DEPENDENCIES_ITEM`, `FINGA_HTTP_ADDR`. The
binaries read their file and their environment, nothing else. For local
overrides, copy `server/.env.example` to `server/.env` (git-ignored): `make
up` passes it to every service's container (Compose's `env_file`) and `make
run` exports it before starting the binaries. To change one service only, a
git-ignored `config.local.yaml` beside its `config.yaml` replaces it, and
`CONFIG_PATH` names any file. Nothing committed is a secret.

Every service has `shutdown_grace` and `log_level`, and each internal
service `grpc.listen` and `ops.addr` (the ports table above). Besides those:

| Binary      | Keys                                                                              |
|-------------|-----------------------------------------------------------------------------------|
| `gateway`   | `http.addr`, `http.cors_origins`; `dependencies` on the five services             |
| `account`   | `database.url`, `database.max_conns`                                              |
| `character` | `database`, `data_dir`, `dependencies.item`                                       |
| `item`      | `database`, `data_dir`                                                            |
| `world`     | `data_dir`, `tick_rate_hz` (20), `checkpoint_interval` (30s), `economy.workers` (8), `dependencies` on character, item and social |
| `social`    | none                                                                              |
| `migrate`   | `database`, `log_level` only                                                      |

## Tests

```bash
make test    # every test, with the race detector
make test-service SERVICE=item   # one service built and tested on its own, as CI's matrix does
make cover   # coverage report over server/internal/, server/pkg/, server/services/ and the composition roots; fails below 80%
make lint    # golangci-lint (sloglint, depguard import directions, revive, staticcheck) and buf lint
make breaking   # buf breaking: the gRPC contracts against the previous commit
make smoke   # the packaging check, against a running stack
make bots    # the proof session: 4 bots play for 30 minutes and the tick p99 is printed (read from the world service's ops port, 9104)
```

The tests that need a database start a Postgres container through
Testcontainers (set `PGTEST_URL` to use a server you run yourself). Among them:

- property tests over the inventory rules (`pgregory.net/rapid`) with the
  Stage 1 item invariants checked after every run;
- the map tick under `testing/synctest`, with a seeded random generator so a
  fight replays exactly;
- end-to-end rows per Stage 1 feature in `server/test/`, each named after
  its feature, against the server built the way production builds it;
- the crash test (`TestCrashDuringPickups`): every service built and run as
  its own process, three test clients looting concurrently, the world and
  item services killed without warning while the gateway keeps the sockets,
  both restarted, every client joined again, and the item invariants
  checked. CI runs it with
  `make test`.

## Architecture at a glance

Six services in one Go module in `server/` (Low-level design, page 10), each
a binary of its own (`server/services/<name>`, its `main.go` at the
service's root), calling each
other over gRPC with the contracts in `server/api/proto/internal/` and the
generated code committed in `server/api/gen/`:

- **`gateway`** is the only public entry: REST under `/api` (contract in
  `server/api/openapi.yaml`, checked for drift by a test) and the WebSocket
  gateway at `/ws` (the message catalogue is `internal/protocol`, with golden
  files in `server/testdata/contracts/` and `server/api/asyncapi.yaml`). It
  holds no database and no game rules: the Hello handshake, one connection
  per account, token rotation, per-account rate limits, and a command queue
  per connection whose goroutine calls the service that owns each message,
  with the identity taken from the connection, never from the message; the
  read loop only decodes, limits and routes. Players join through the
  world, whose events arrive on one delivery stream per gateway; when the
  world restarts, every socket stays open and each player is joined again.
  `errmap` turns the reason a service's failure carries into what a client
  sees: an HTTP status, a code of its own (`party_full`) and the player
  message; a failure it does not know answers `internal` and is logged. On
  SIGTERM the gateway turns unready, tells every connection to reconnect
  (Kicked `server_restart`), then stops.
- **`account`** holds accounts and sessions. The gateway checks tokens
  through `pkg/auth`'s `Guard` over its client, and its `WatchSessions`
  stream tells the gateway which connections to close when a session ends.
- **`character`** holds the characters: REST lists and creates them through
  it, and the world loads them on join and saves checkpoints and level-ups
  to it. A new character's starter items are granted through the item
  service after the character commits.
- **`item`** holds the items: append-only history, where every change is an
  item event and three invariant checks (`services/item/invariants`) tie
  quantities to events.
- **`world`** runs one actor per map at 20 Hz: inputs applied in sequence
  order, movement in fixed sub-steps shared with the client as test
  vectors, mobs, combat, EXP, drops, portals, and a delta to every player
  each tick. A tick never waits on another service: pickups go to the
  economy worker pool, which calls the item service with the drop's ID as
  the idempotency key, and the result comes back as an inbox message;
  checkpoints go to the character service from the persistence worker;
  party membership is a local copy fed by the social service.
- **`social`** holds the parties and chat, in memory: it delivers party
  state, invites and party chat to each member's gateway on its own stream,
  and feeds the world's party copy on `WatchParties`.

Inside a service, `main.go` starts it and `app/` is its composition root (the end-to-end
tests build the whole game from these, exactly as production does),
`config/` its own configuration, and `internal/` its layers, one package
each (Low-level design, page 11): `model` (entities, value types and the
errors), `application` (the use cases, and the ports they need),
`repository` (Postgres through sqlc and pgx, no business rules in SQL),
`handler` (its gRPC server and error table) and `clients` (its adapters for
the services it calls); imports point inward, to `model`. Every service
but the gateway is in layers so far; the gateway keeps its handlers'
packages until step 36. `migrations/` holds its
own goose files with its own version table. Go's
`internal/` rule keeps services out of each other, and depguard checks the
rest of the import direction: composition roots → services → the shared
kernel (`internal/kernel`, `internal/gamedata`, `internal/sim`,
`internal/progression`) → `pkg/`, the generic parts (config loading, the
transaction helper with retries, idempotency keys, the outbox, token
buckets, the tick loop, the WebSocket connection, typed IDs, Tiled maps, the
gRPC plumbing, Testcontainers glue).

- **Persistence**: character state checkpoints every 30 s and on level-up,
  map change and logout; items never wait for a checkpoint. Sessions are
  opaque tokens stored hashed, rotated on every Hello, with reuse detection
  and a sliding expiry (page 4's Trust model).

## How this was built with AI

The backend was written step by step with an AI coding assistant working from
the design pages, under rules the design set: read the step page, write the
listed tests first, build only the step's scope, keep `make test`, `make
lint` and `make cover` green, one commit per step, and when a step shows a
design page is wrong, fix the page in the same step so the pages always say
what the code does.

`docs/ai-coding-log.md` is the trail: one row per step (what the tests
cover, what was built, how it was verified, what the user changed, what the
tests caught), plus every decision taken while the user was away, numbered,
with the reason. The caught bugs are kept on purpose, from a joined
invariant query that multiplied quantities, to mobs that drifted across the
whole map because the patrol rule was read too literally, to a session
revoked in the database while its connection stayed open. The review
happened at milestone ends, against that log and the design pages, which
moved from Notion into `docs/` on 2026-10-23.
