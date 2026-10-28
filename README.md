# Finga

A browser-based 2D side-scrolling MMORPG-lite: pick a class, hunt mobs, level
up, team up in parties of up to 4, and loot. Go game server, Postgres, and a
Phaser client. The design lives in `docs/`: the product (what the game is) and
the tech design (how it is built), starting at `docs/README.md`.

This repository holds Stage 1, the proof of concept: the backend and the
client, built in the steps of `docs/tech/lld/8-backend-implementation-steps.md`
and `docs/tech/lld/9-client-implementation-steps.md`, one commit per step,
with the evidence trail in `docs/ai-coding-log.md` and the proof-session
checklist in `docs/proof-session.md`.

## Repository layout

```txt
server/   the Go module: the game server and its tools, migrations, configuration,
          the game content (data/), and the contracts (api/, testdata/)
web/      the browser client: Phaser 4, React, Vite, TypeScript
infra/    how it runs: Docker Compose today; infrastructure as code and the
          Grafana dashboards join here
docs/     the design (product and tech), the AI coding log, the proof-session checklist
```

The server is the reference for everything the two sides share: the client
generates its game data from `server/data/`, copies the maps from there, and
its tests check `server/testdata/` (the message golden files, the movement
vectors, the error codes). The root `Makefile` drives all three parts.

## Build and run

```bash
make up      # build the images, start Postgres, run the migrations, start the game server on :8080
make smoke   # a client signs up, enters the world and moves, against the running stack
make down    # stop everything and drop the database volume
```

`make up` works from a clean clone with Docker and Compose only. When another
Postgres owns port 5432 on the host, set `FINGA_POSTGRES_PORT` in
`infra/.env` (copy `infra/.env.example`); the server's own host port is
`FINGA_PORT` (default 8080). Without Docker, run Postgres yourself and start
the binaries:

```bash
make run                   # exports server/.env, applies every service's migrations to FINGA_DATABASE_URL, starts the services and the game server
```

The game server serves REST under /api, the gateway at /ws, /healthz,
/readyz and /metrics.

Prerequisites for working on the code: Go 1.27 or newer (a C compiler for the
race detector; on Windows, for example `winget install
BrechtSanders.WinLibs.POSIX.UCRT`), make, golangci-lint v2 for `make lint`,
sqlc for `make gen` (which also runs `buf generate` through the Go tools pinned in `server/go.mod`, so nothing else needs installing), and Docker for the tests that run a real Postgres.

## Client

The browser client lives in `web/` (Phaser 4, React 19, Vite 8, TypeScript,
Zustand, Vitest; page 7 of the design). It is built step by step after the
backend, the same way, with its own rows in `docs/ai-coding-log.md`.

```bash
make up                         # the server on :8080
npm --prefix web install        # once; Node 24 (web/.nvmrc)
npm --prefix web run dev        # the client on :5173, /api and /ws proxied to the server
make play                       # for a play session on the LAN: make up, the production build, vite preview on :4173 (--host), same proxy
```

`make test`, `make lint` and `make cover` run the client's scripts too
(`npm --prefix web test`, `run lint`, `run coverage`), so one command checks
both halves. The lint step includes a fixture that must be rejected by the
layer rule (page 7's five layers, enforced with `import-x/no-restricted-paths`).

## Configuration

Defaults live in `server/configs/config.yaml`, and the binaries read their
paths from where they run, `server/`. Any key can be overridden by an
environment variable named `FINGA_` plus its path in upper case, for example
`FINGA_SERVER_ADDR=:9090`. The binaries read the YAML file and their
environment, nothing else. For local overrides, copy `server/.env.example`
to `server/.env` (git-ignored): `make up` passes it to the game server
container (Compose's `env_file`) and `make run` exports it before starting the
binaries. A `server/configs/config.local.yaml` (git-ignored) replaces the
defaults file when present, and `CONFIG_PATH` names any file. Nothing
committed is a secret.

| Section     | Keys                                                            |
|-------------|-----------------------------------------------------------------|
| `server`    | `addr`, `cors_origins`, `shutdown_grace`                        |
| `database`  | `url`, `max_conns`                                              |
| `world`     | `tick_rate_hz` (20), `checkpoint_interval` (30s), `data_dir`    |
| `economy`   | `workers` (8): the pool that runs pickups and inventory actions |
| `telemetry` | `log_level`                                                     |
| `services`  | `listen`, `addr`, `ops` for account, character, item, world, social: each internal service's gRPC listener, where callers dial it, and its `/healthz`, `/readyz`, `/metrics` |

## Tests

```bash
make test    # every test, with the race detector
make cover   # coverage report over server/internal/, server/pkg/, server/services/ and the composition roots; fails below 80%
make lint    # golangci-lint (sloglint, depguard import directions, revive, staticcheck) and buf lint
make smoke   # the packaging check, against a running stack
make bots    # the proof session: 4 bots play for 30 minutes and the tick p99 is printed
make play    # the proof session with people: the server plus the client preview build on the LAN (:4173)
```

The tests that need a database start a Postgres container through
Testcontainers (set `PGTEST_URL` to use a server you run yourself). Among them:

- property tests over the inventory rules (`pgregory.net/rapid`) with the
  Stage 1 item invariants checked after every run;
- the map tick under `testing/synctest`, with a seeded random generator so a
  fight replays exactly;
- end-to-end rows per Stage 1 feature in `server/test/`, each named after
  its feature, against the server built the way production builds it;
- the crash test (`TestCrashDuringPickups`): the item service and
  `server/cmd/gameserver` built and run as their own processes, three test
  clients looting concurrently, both processes killed without warning,
  restarted, and the item invariants checked. CI runs it with `make test`.

## Architecture at a glance

One Go module in `server/`, laid out so that imports only point one way
(depguard checks it): `cmd` → `internal/handler` → `internal/application`
→ the shared kernel (`internal/model`, `internal/gamedata`, `internal/sim`,
`internal/progression`) → `pkg`.

- **`cmd/gameserver/app`** is the game server's composition root: `Build`
  wires every module from configuration, and `Server` listens, runs the
  background tasks and shuts down gracefully. Each binary wires its own
  dependencies next to its main; this one is a package of its own so the
  end-to-end tests can build the server exactly as production does.
- **`internal/handler/router.go`** is the route table: REST under `/api`,
  the gateway at `/ws`, `/metrics`, `/healthz`, and `/readyz`, which
  says whether the process can take traffic and checks no dependency.
- **`internal/handler/api`** is the REST API (sign up, log in, characters,
  settings; contract in `server/api/openapi.yaml`, checked for drift by a test).
- **`internal/handler/ws`** is the WebSocket gateway: the Hello handshake, one
  connection per account, token rotation, per-account rate limits, and routing
  of every message to the character's map instance with the identity taken
  from the connection, never from the message. The message catalogue is
  `internal/protocol`, with golden files in `server/testdata/contracts/` and
  `server/api/asyncapi.yaml`.
- **`internal/handler/errmap`** turns the errors services return, plain
  sentinels named for the failure such as `party.ErrFull`, into what a
  client sees: an HTTP status, a code of its own (`party_full`) and the
  player message. An error it does not know answers `internal` and is
  logged with its cause.
- **`internal/application/world`** runs one actor per map at 20 Hz: inputs
  applied in sequence order, movement in fixed sub-steps shared with the
  client as test vectors, mobs, combat, EXP, drops, portals, and a delta to
  every player each tick. A tick never waits on the database: pickups go to
  the economy worker pool, which calls the item service with the drop's ID as
  the idempotency key, and the result comes back as an inbox message.
- **`services/item`** is the first service in its own process (Low-level
  design, page 10): the item module in `internal/item`, its gRPC handler,
  its migrations, and the invariant checks in `invariants`. Items are
  append-only history: every change is an item event, and three invariant
  checks tie quantities to events. The game server calls it through the
  generated client in `api/gen/item/v1`.
- **`services/account`** runs accounts and sessions the same way; the
  REST handler and the gateway check tokens through `pkg/auth`'s `Guard`
  over its client, and its `WatchSessions` stream tells the gateway which
  connections to close when a session ends.
- **`services/social`** holds the parties and chat, in memory: it delivers
  party state, invites and party chat to each member's gateway on its own
  stream, and the world's ticks read a local copy of party membership fed by
  its `WatchParties` stream.
- **`services/character`** holds the characters: REST lists and creates
  them through it, Hello loads them, and the world's persistence worker
  saves checkpoints and level-ups to it. A new character's starter items
  are granted through the item service after the character commits.
  With it, the game server needs no database of its own.
- **`pkg/`** holds the generic parts: config loading, the transaction
  helper with retries, idempotency keys, the outbox, token
  buckets, the tick loop, the WebSocket connection, typed IDs, Tiled maps,
  Testcontainers glue.
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
