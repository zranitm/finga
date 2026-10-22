# Finga

A browser-based 2D side-scrolling MMORPG-lite: pick a class, hunt mobs, level
up, team up in parties of up to 4, and loot. Go game server, Postgres, and a
Phaser client that comes after this backend. The design lives in Notion and
is exported to `docs/`.

This repository holds Stage 1, the proof of concept: the backend built in the
18 steps of Low-level design page 8, one commit per step, with the evidence
trail in `docs/ai-coding-log.md` and the proof-session checklist in
`docs/proof-session.md`.

## Build and run

```bash
make up      # build the images, start Postgres, run the migrations, start the game server on :8080
make smoke   # a client signs up, enters the world and moves, against the running stack
make down    # stop everything and drop the database volume
```

`make up` works from a clean clone with Docker and Compose only. When another
Postgres owns port 5432 on the host, set `FINGA_POSTGRES_PORT`; the server's
own port is `FINGA_PORT` (default 8080). Without Docker, run Postgres yourself
and start the binaries:

```bash
go run ./cmd/migrate       # applies migrations/ to FINGA_DATABASE_URL
go run ./cmd/gameserver    # REST under /api, the gateway at /ws, /healthz, /readyz, /metrics
```

Prerequisites for working on the code: Go 1.27 or newer (a C compiler for the
race detector; on Windows, for example `winget install
BrechtSanders.WinLibs.POSIX.UCRT`), make, golangci-lint v2 for `make lint`,
sqlc for `make gen`, and Docker for the tests that run a real Postgres.

## Configuration

Defaults live in `configs/config.yaml`. Any key can be overridden as `FINGA_`
plus its path in upper case, for example `FINGA_SERVER_ADDR=:9090`. Copy
`.env.example` to `.env` (git-ignored) for local overrides; precedence, lowest
to highest: the YAML file, `.env`, real environment variables. A
`configs/config.local.yaml` (git-ignored) replaces the defaults file when
present, and `CONFIG_PATH` names any file. Nothing committed is a secret.

| Section     | Keys                                                            |
|-------------|-----------------------------------------------------------------|
| `server`    | `addr`, `cors_origins`, `shutdown_grace`                        |
| `database`  | `url`, `max_conns`                                              |
| `world`     | `tick_rate_hz` (20), `checkpoint_interval` (30s), `data_dir`    |
| `economy`   | `workers` (8): the pool that runs pickups and inventory actions |
| `telemetry` | `log_level`                                                     |

## Tests

```bash
make test    # every test, with the race detector
make cover   # coverage report over internal/ and pkg/; fails below 80%
make lint    # golangci-lint: sloglint, depguard import directions, revive, staticcheck
make smoke   # the packaging check, against a running stack
make bots    # the proof session: 4 bots play for 30 minutes and the tick p99 is printed
```

The tests that need a database start a Postgres container through
Testcontainers (set `PGTEST_URL` to use a server you run yourself). Among them:

- property tests over the inventory rules (`pgregory.net/rapid`) with the
  Stage 1 item invariants checked after every run;
- the map tick under `testing/synctest`, with a seeded random generator so a
  fight replays exactly;
- end-to-end rows per Stage 1 feature in `test/`, each named after its
  feature, against the server built the way production builds it;
- the crash test (`TestCrashDuringPickups`): `cmd/gameserver` built and run as
  its own process, three test clients looting concurrently, the process
  killed without warning, restarted, and the item invariants checked. CI runs
  it with `make test`.

## Architecture at a glance

One Go module, laid out so that imports only point one way (depguard checks
it): `cmd` → `internal/server` → `internal/handler` → `internal/application`
→ `internal/model` → `pkg`.

- **`internal/handler/api`** is the REST API (sign up, log in, characters,
  settings; contract in `api/openapi.yaml`, checked for drift by a test).
- **`internal/handler/ws`** is the WebSocket gateway: the Hello handshake, one
  connection per account, token rotation, per-account rate limits, and routing
  of every message to the character's map instance with the identity taken
  from the connection, never from the message. The message catalogue is
  `internal/protocol`, with golden files in `testdata/contracts/` and
  `api/asyncapi.yaml`.
- **`internal/application/world`** runs one actor per map at 20 Hz: inputs
  applied in sequence order, movement in fixed sub-steps shared with the
  client as test vectors, mobs, combat, EXP, drops, portals, and a delta to
  every player each tick. A tick never waits on the database: pickups go to
  the economy worker pool with the drop's ID as the idempotency key, and the
  result comes back as an inbox message.
- **`internal/application/{account,character,item,party,chat}`** are the
  modules, each a service over a repository (sqlc, pgx) with no business
  rules in SQL. Items are append-only history: every change is an item event,
  and three invariant checks tie quantities to events.
- **`pkg/`** holds the generic parts: config loading, typed errors, the
  transaction helper with retries, idempotency keys, the outbox, token
  buckets, the tick loop, the WebSocket connection, Testcontainers glue.
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
happened at milestone ends, against that log and the Notion pages.
