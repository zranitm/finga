# Finga

A browser-based 2D side-scrolling MMORPG-lite: pick a class, hunt mobs, level
up, team up in parties of up to 4, and loot. Go services, Postgres, and a
Phaser client. The product design (what the game is) lives in `docs/`,
starting at `docs/README.md`.

This repository holds Stage 1, the proof of concept: the backend and the
client, with the proof-session checklist in `docs/proof-session.md`.

## Repository layout

```txt
server/   the Go module: the six services (services/), their tools, configuration,
          the game content (data/), and the contracts (api/, testdata/)
web/      the browser client: Phaser 4, React, Vite, TypeScript
infra/    how it runs: Docker Compose, the nginx config, the Grafana stack, and
          production on AWS (deploy/, terraform/; deploy/README.md is the runbook)
docs/     the product design, the proof-session checklist
```

The server is the reference for everything the two sides share: the client
generates its game data from `server/data/` and its error codes from
`server/testdata/errors/codes.json`, copies the maps from there, and its tests
check `server/testdata/` (the message golden files, the movement vectors, the
error codes). The root `Makefile` drives all three parts.

## Build and run

```bash
make up        # everything: Postgres, the migrations, the six services and the client; the game at http://localhost:4173
make up OBS=1  # the same, plus Grafana at http://localhost:3000 over the services' metrics, logs and traces
make smoke     # a client signs up, enters the world and moves, against the running stack
make edge      # nginx limits each client address, the gateway does not (uses up the login allowance for a minute)
make testers   # the test accounts below, high level and ready to play, on the running stack
make down      # stop everything and drop the database volume
```

`make up` is the one command: it works from a clean clone with Docker and
Compose only, the client included: Docker Engine 25 or newer and Compose
2.24 or newer, for the health checks' `start_interval` and the optional
`env_file` (`required: false`). The `web` image builds the client with
Node and serves it with nginx, which passes `/api` and `/ws` to the
gateway, so the browser sees one origin. Testers on the same network open
`http://<this machine's LAN address>:4173`. When another program owns a
port the stack publishes, set `FINGA_WEB_PORT` (4173),
`FINGA_PORT` (8080), `FINGA_GATEWAY_OPS_PORT` (9100) or
`FINGA_POSTGRES_PORT` (5432) in `infra/.env` (copy `infra/.env.example`).
The services ship together: every one runs the same commit, and a mix of
versions is not supported. A contract can change its meaning without changing
its shape, which `make breaking` cannot see, so a commit that does is marked
`!` with a `BREAKING-CHANGE:` footer. On a running stack `make up` builds
first, then stops every service, callers before the services they call, so
each one's last saves reach the version it was built against, and starts the
new ones.

### Test accounts

`make testers` keeps these accounts on the running stack (the one `make up`
made, or `COMPOSE_PROJECT_NAME`'s), each with the password `testers-only`:

| Username        | Character      | Ready for                                                                                       |
| --------------- | -------------- | ----------------------------------------------------------------------------------------------- |
| `test_warrior`  | TestWarrior    | high-level play: a Warrior at the level cap (30), in town                                       |
| `test_thief`    | TestThief      | the same as a Thief                                                                             |
| `test_bowman`   | TestBowman     | the same as a Bowman                                                                            |
| `test_magician` | TestMagician   | the same as a Magician                                                                          |
| `test_promote`  | TestPromote    | a promotion: a level 10 Beginner whose Warrior's Trial is done; hand it in, then promote         |
| `test_new`      | TestNew        | the start: a level 1 Beginner                                                                   |

The four at the cap have their stat points spent by their class's split and
their skill points as the bots spend them. They wear the best gear each slot
takes at their level, hold a stack of White Potions, and start with full energy.
Accounts and characters are made the way a player makes them. Levels and
items are written through the services that own them, as the world does, so
the item checks hold.

A wiped stack (`make down`, or a fresh database after a breaking change) loses
them: run `make testers` again for the same accounts. On a stack that has
them it changes nothing, but it logs each one in to check, which ends a
session open on that account. It runs in a one-off container on the stack's
own network and refuses a gateway that isn't on this machine.

Without Docker, run Postgres yourself, start the binaries, and serve the
client with the Vite dev server (see Client):

```bash
make run                   # exports server/.env, applies every service's migrations to FINGA_DATABASE_URL, starts the six services
```

The browser talks to nginx, and nginx to the gateway. The gateway serves
REST under /api and the WebSocket at /ws on :8080, and /healthz, /readyz and
/metrics on its ops port; every other service serves gRPC on one port and the
same three on its ops port:

| Process         | gRPC | HTTP / ops                                  | On the host with `make up`                                                             |
|-----------------|------|---------------------------------------------|----------------------------------------------------------------------------------------|
| web (nginx)     |      | 80: the client; /api and /ws to the gateway | 4173 (`FINGA_WEB_PORT`): open this one                                                 |
| gateway         |      | 8080: /api, /ws; 9100                       | 8080 (`FINGA_PORT`), for `make smoke` and `make bots`; 9100 (`FINGA_GATEWAY_OPS_PORT`) |
| account         | 9001 | 9101                                        |                                                                                        |
| character       | 9002 | 9102                                        |                                                                                        |
| item            | 9003 | 9103                                        | 9103 (`FINGA_ITEM_OPS_PORT`), for `make bots`                                          |
| world           | 9004 | 9104                                        | 9104 (`FINGA_WORLD_OPS_PORT`), for `make bots`                                         |
| social          | 9005 | 9105                                        |                                                                                        |
| Postgres        |      | 5432                                        | 5432 (`FINGA_POSTGRES_PORT`)                                                           |
| Vite dev server |      | 5173 (`npm run dev`)                        | not in Compose; for working on the client                                              |

Under `make up` the services reach each other on the Compose network by
name (`world:9004`), and only the ports in the last column are published,
all but the client's on 127.0.0.1;
under `make run` every port is on localhost. The defaults live in
`server/config/config.yaml` (see Configuration).

Prerequisites for working on the code: Go 1.27 or newer (a C compiler for the
race detector; on Windows, for example `winget install
BrechtSanders.WinLibs.POSIX.UCRT`), make, golangci-lint v2 for `make lint`,
and Docker for the tests that run a real Postgres. `make gen` runs sqlc and
buf through the Go tools pinned in `server/go.mod`, so nothing else needs installing.

## Client

The browser client lives in `web/` (Phaser 4, React 19, Vite 8, TypeScript,
Zustand, Vitest). It is built step by step after the backend, the same way.

`make up` already serves the client's production build on :4173. To work
on the client with hot reload, run the dev server against the running stack:

```bash
npm --prefix web install        # once; Node 24 (web/.nvmrc)
npm --prefix web run dev        # the client on :5173, /api and /ws proxied to the gateway on :8080 (make up or make run)
```

`make test`, `make lint` and `make cover` run the client's scripts too
(`npm --prefix web test`, `run lint`, `run coverage`), so one command checks
both halves. The lint step includes a fixture that must be rejected by the
layer rule (the client's five layers, enforced with `import-x/no-restricted-paths`).

## Configuration

One file holds the configuration of every service: the defaults in
`server/config/config.yaml`, the sections every service shares first
(`http`, `ops`, `shutdown_grace`, `log`, `tracing`, `database`, `data_dir`), then
one section per module, each checked by its Go type (`server/config/`, and
each service's `config/` package for its own section). Migrate keeps its own
file in `server/cmd/migrate/config/`. Every binary reads the whole file
from where it runs, `server/`, and refuses a key in the file that no field
names. Any key can be overridden by an environment variable named `FINGA_` plus its path in
upper case: `FINGA_DATABASE_URL`, `FINGA_HTTP_ADDR`,
`FINGA_WORLD_DEPENDENCIES_ITEM`. A `FINGA_` variable that overrides no key
is ignored, with a warning at startup, so a typo shows in the log. The
binaries read their file and their environment, nothing else. For local overrides, copy `server/.env.example`
to `server/.env` (git-ignored): `make up` passes it to each of the six
services' containers (Compose's `env_file`; migrate takes only the database
settings) and `make run` exports it before starting the binaries. A
git-ignored `config.local.yaml` beside `config.yaml` replaces it, and
`CONFIG_PATH` names any file. Nothing committed is a secret.

The game data in `data_dir` is loaded once per process. `cmd/finga` runs
every module in one process, serving players at `http.addr` and its health
and metrics at the top-level `ops.addr` (9100). Each module's section also
holds where its own binary listens, `ops.addr` and for an internal service
`grpc.listen` (the ports table above), and `dependencies`, the services it
dials; `cmd/finga` uses none of them. Besides those:

| Section     | Keys                                                                              |
|-------------|-----------------------------------------------------------------------------------|
| `gateway`   | `ws.hello_timeout` (5s), `ws.command_queue` (64); `sessions.join_timeout` (5s), `sessions.check_interval` (1m); `dependencies` on the five services. It serves players at the shared `http.addr` and `http.cors_origins` |
| `account`   | `password` (`time`, `memory_kib`, `threads`, `concurrent`, `wait`), the Argon2id cost; `sessions` (`keep` 720h, `prune_*`) |
| `character` | `dependencies.item`                                                               |
| `item`      | `idempotency_keys` (`keep` 24h, `prune_*`); `outbox` (`published_keep` 24h, `unpublished_keep` 168h, `prune_*`) |
| `world`     | `tick_rate_hz` (20), `checkpoint_interval` (30s), `flush_timeout` (10s), `item_calls.workers` (8), `item_calls.batch_max` (32, at most the item service's 32), `dependencies` on character, item and social |
| `social`    | `members` (`keep` 1h, `prune_*`)                                                  |

Account, character and item use the shared `database`; migrate reads
`database` and `log` from its own file.

## Tests

```bash
make test    # every test, with the race detector
make test-service SERVICE=item   # one service built and tested on its own, as CI's matrix does
make cover   # coverage report over server/internal/, server/pkg/, server/services/ and the composition roots; fails below 80%
make lint    # golangci-lint (sloglint, depguard import directions, revive, staticcheck) and buf lint
make breaking   # buf breaking: the gRPC contracts against the previous commit
make smoke   # the packaging check, against a running stack
make edge    # nginx limits each client address, the gateway does not; run it last
make bots    # the proof session: 4 bots play for 30 minutes (BOTS_DURATION) toward their first classes, and the promotions and tick p99 are printed (read from the world service's ops port, 9104)
```

The tests that need a database start a Postgres container through
Testcontainers (set `PGTEST_URL` to use a server you run yourself). Among them:

- property tests over the inventory rules (`pgregory.net/rapid`) with the
  Stage 1 item invariants checked after every run;
- the map tick under `testing/synctest`, with the instance's random source
  fixed so a fight replays exactly;
- end-to-end rows per Stage 1 feature in `server/test/`, each named after
  its feature, against the server built the way production builds it;
- the crash test (`TestCrashDuringPickups`): the game built and run as its
  own process from `cmd/finga`, three test clients looting while the item
  module beside it keeps swapping their weapons, the game killed without
  warning and restarted, every client reconnected and welcomed with what the
  database holds, and the item invariants checked;
- the item failure test (`TestGrantsTheItemModuleFailsLandOnceEach`): the
  world's grants meet an item module that fails some calls, before and after
  they commit, and every drop lands once. CI runs both with `make test`.

## Architecture at a glance

Six services in one Go module in `server/`, each
a binary of its own (its `main.go` in `server/cmd/<name>`), calling each
other over gRPC with the contracts in `server/api/proto/internal/` and the
generated code committed in `server/api/gen/`. `cmd/finga` runs them all as
modules of one process instead, each calling the others' facades, the
packages at `server/services/<name>`; Compose and the deploy still run the
six binaries:

- **`gateway`** is the only public entry, on Gin: REST under `/api`
  (contract in `server/api/openapi.yaml`, checked for drift by a test) and the WebSocket
  gateway at `/ws` (binary Protobuf messages defined once in
  `server/api/proto/client/v1/` and generated for Go and TypeScript, with an
  example of each in `server/testdata/contracts/`). It
  holds no database and no game rules: the Hello handshake, one connection
  per account, token rotation, per-account rate limits, and a command queue
  per connection whose goroutine calls the service that owns each message,
  with the identity taken from the connection, never from the message. The
  WebSocket handler is built like the REST one: each message type is one
  route with its middleware (Hello first, rate limit, the queue, the reply)
  on `pkg/wsrouter`, a router for WebSocket messages as Gin is for HTTP, and
  one handler that turns the message into a use case's arguments and its
  result into the reply; the read loop only decodes, limits and routes. The
  application is two flows over the sessions they share: `send`, what a
  connection asks for, and `push`, what the services' streams start. They
  tell the player what happened through an `Outbox` port (Welcome, the
  services' pushes, and why the session ends), which the handler puts in
  protocol messages and close codes. Players join through the
  world, whose events arrive on one delivery stream per gateway; when the
  world restarts, every socket stays open and each player is joined again.
  Its adapters' `errmap` turns the reason a service's failure carries into
  what a client sees: a six-digit code (440002, `PARTY_FULL`), to which the
  REST handler adds an HTTP status and a message for developers; a failure it
  does not know answers `INTERNAL` and is logged. A code's first two digits
  are its area (10 requests, 11 accounts, 12 characters, 22 items, 33 the
  world and quests, 44 parties and chat), and each area counts up in
  `model/failures.go`; a code is never reused. The client words every code
  itself, and one it doesn't know shows its number. In layers, its model has
  its own views of what a player is shown, and its use cases (the REST API's
  Lobby, the connections' Gateway) know neither gRPC nor HTTP nor the
  socket, nor any client message. On
  SIGTERM the gateway turns unready, tells every connection to reconnect
  (Kicked `server_restart`), then stops.
- **`account`** holds accounts and sessions. The gateway checks tokens
  through its own session ports, served by its account client, and its
  `WatchSessions` stream tells the gateway which connections to close
  when a session ends.
- **`character`** holds the characters: REST lists and creates them through
  it, and the world loads them on join and saves checkpoints and level-ups
  to it. A new character's starter items are granted through the item
  service after the character commits.
- **`item`** holds the items: append-only history, where every change is an
  item event and three invariant checks, run by the tests
  (`services/item/itemtest`), tie quantities to events. Grants, loots,
  discards, uses and hand-ins also write an event to the outbox, for Stage
  2's relay to publish. A published row is deleted a day after
  `published_at`. In Stage 1 nothing publishes, so `outbox.unpublished_keep`
  deletes unsent rows after 7 days. When the relay arrives, set it to 0
  (off), or the deletion loses events the relay hasn't sent.
- **`world`** runs one actor per map at 20 Hz: inputs applied in sequence
  order, movement in fixed sub-steps shared with the client as test
  vectors, mobs, combat, EXP, drops, portals, and a delta to every player
  each tick. A tick never waits on another service: pickups go to the
  item-call worker pool, which calls the item service with the drop's ID as
  the idempotency key, and the result comes back as an inbox message;
  checkpoints go to the character service from the persistence worker;
  party membership is a local copy fed by the social service.
- **`social`** holds the parties and chat, in memory behind a store port
  that a Redis or Postgres repository can replace: it delivers party
  state, invites and party chat to each member's gateway on its own stream,
  and feeds the world's party copy on `WatchParties`.

Inside account, character, item, world and social, the service's root package is
its facade, the module's contract: Go types for its requests, results and
events, and a method per call, each traced, counted and recovered by
`internal/facade`. A module that streams owns the stream's hub, and
`Subscribe` hands a subscriber its own events. Failures reach callers as kernel errors: a
`kernel.Rule` per contract reason, or `transient.ErrUnavailable`, which
a later try may clear. No retry passes a rule but a retryable one, a
module busy or stopping, which is transient too.
`app/` is a service's composition root (the end-to-end
tests build the whole game from these, exactly as production does),
`config/` the type of its section of `server/config/config.yaml`, and `internal/` its layers, one package
each: `model` (entities, value types and the
errors), `application` (the use cases, and the ports they need),
`repository` (Postgres through sqlc and pgx, no business rules in SQL),
`handler` (its gRPC server over the facade, and the table from kernel
errors to gRPC codes and reasons) and `clients` (its adapters for
the services it calls); imports point inward, to `model`. `migrations/` holds its
own goose files with its own version table. Go's
`internal/` rule keeps services out of each other, and depguard checks the
rest of the import direction: composition roots → services → the shared
kernel (`internal/kernel`, `internal/gamedata`, `internal/sim`,
`internal/progression`) → `pkg/`, the generic parts (config loading, the
transaction helper with retries on `cenkalti/backoff`, idempotency keys, the outbox, keyed rate
limits on `golang.org/x/time/rate`, the tick loop, the WebSocket connection and its message router, typed IDs, Tiled maps, the
gRPC plumbing, Testcontainers glue).

- **Persistence**: character state checkpoints every 30 s and on level-up,
  map change and logout; items never wait for a checkpoint. Sessions are
  opaque tokens stored hashed, rotated on every Hello, with reuse detection
  and a sliding expiry.

## How this was built with AI

The backend was written step by step with an AI coding assistant, under a
few rules: write the tests first, build only the step's scope, keep `make
test`, `make lint` and `make cover` green, and make one commit per step.

The git history is the trail: each step is one Conventional Commit
(`feat:`, `fix:`, `refactor:`, `docs:`, ...) whose message says what changed,
why, and how it was verified.
