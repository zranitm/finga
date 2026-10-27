# 10. Gateway and services

Stage 1 was built as one binary, `cmd/gameserver`, with every module in one process. On 2026-10-27 the user decided to split it already in Stage 1: the game runs as separate processes behind a connection gateway, so it is the base for scaling later. Stage 1 runs one node per service; Stage 2 adds more nodes, a registry, and NATS (TD-25).

This page is the plan for the split: the services and what each owns, the layout, the internal contracts, the flows that change, what happens when a service restarts, and the steps that build it (backend steps 20 to 28). Pages 1 to 4 are updated by each step as the code changes, so they always say what the code does; this page says where the steps are going.

## Why

The reason is deploys. A game service (world, item, character, social, account) can restart without dropping a socket: the gateway keeps every WebSocket open, and a world restart is a short freeze while the gateway rejoins its players. Only the gateway itself, which holds the sockets, needs a rolling drain. Rule or content changes, client updates, and big migrations still go in a scheduled maintenance window.

## Services

| Service | Owns | State | Tables | Calls |
| --- | --- | --- | --- | --- |
| `gateway` | WebSocket connections, REST entry, routing, per-player command queues, rate limits | Connections only | None | every other service |
| `account` | Accounts, sessions, tokens, settings | None in memory | `accounts`, `sessions` | nothing |
| `character` | Characters, stats, levels, checkpoints | None in memory | `characters` | item (starter items) |
| `item` | Items, inventory, item events, idempotency, outbox. Stage 2: economy and market join here, since they share transactions with items | None in memory | `items`, `item_events`, `idempotency_keys`, `outbox` | nothing |
| `world` | Map instances, movement, mobs, combat, drops, portals | Stateful: one owner per map | None | character, item, social |
| `social` | Parties, invites, chat | Parties and invites, in memory (TD-19) | None | nothing |

The gateway has no game rules and no database. It is deployed rarely and kept thin.

## Layout

Everything stays inside `server/`, the Go module (R2), and Compose stays in `infra/` (decision 167). The tree after step 28:

```txt
server/
  api/
    openapi.yaml  asyncapi.yaml       the public contracts, served by the gateway
    proto/internal/<service>/v1/      the internal gRPC contracts, one package per service
    gen/<service>/v1/                 generated Go code (committed; make gen)
  services/
    gateway/    cmd/gateway/  internal/   WebSocket connections, REST entry, routing, per-player command queues
    account/    cmd/account/  internal/  migrations/
    character/  cmd/character/  internal/  migrations/
    item/       cmd/item/  internal/  migrations/
    world/      cmd/world/  internal/      map instances, sim, combat, drops, portals
    social/     cmd/social/  internal/     parties, invites, chat (in memory)
  internal/
    model/        Finga's ID types (AccountID, CharacterID, ItemID, SessionID) on pkg/typedid, and Class
    gamedata/     Finga's game-data schema and validation (classes, mobs, items, maps) on pkg/tiled
    sim/          the movement simulation: map geometry for gamedata, the world's movement, the client's vectors
    progression/  derived stats, EXP awards and the death penalty: the character service and the world's tick
    protocol/     the client's WebSocket messages: the gateway, the bot and the end-to-end tests
    config/       the configuration schema every binary shares
    telemetry/    metrics; implements the services' observer interfaces
  pkg/            generic libraries with no game knowledge: configload, dbx, idempotency, outbox, ratelimit, auth,
                  tick, wsconn, observability, pgtest, and new: typedid, tiled, grpcx, runner, hub
  cmd/migrate/    applies every service's migrations, each with its own goose version table
  cmd/bot/        the proof-session bots
  configs/  data/  test/  testdata/
  Dockerfile      one file; the SERVICE build argument picks the binary
infra/docker-compose.yml   Postgres, migrate, and every service
web/  docs/                unchanged
```

Inside a service:

```txt
services/item/
  cmd/item/main.go        loads configuration, builds the service, runs it until SIGINT or SIGTERM
  cmd/item/app/           the composition root: Build wires the module, the gRPC server and the runner
  internal/item/          the module, moved as it is: api.go, service.go, deps.go, repository.go, queries/, internal/db/
  internal/handler/       the gRPC server: proto in, one service call, proto or a status with a reason out
  internal/clients/       gRPC adapters for the services this one calls (character's item client, the world's three)
  migrations/             this service's goose files, embedded; its own version table
```

### Layout rules

- Code that could ship in another project unchanged goes in `pkg/`. Code that knows about Finga but more than one service needs goes in the root `internal/`. Everything else lives inside its service.
- Go's `internal/` rule is the service boundary: `services/X/internal/` is importable only from `services/X/`. A service reaches another only through the client generated from `api/proto/internal/`. depguard also stops one service importing another's `cmd/` (its composition root), which only the end-to-end tests may import.
- Anything crossing services goes through proto messages, not shared Go types. The ID types in `internal/model` are shared, and so are the root packages above (game data, the movement simulation, the progression rules), which are code both sides run, never a type sent between them.
- Each service owns its migrations in `services/X/migrations/`, applied with its own goose version table (`goose_account_version`, ...), all on the one Stage 1 Postgres. No service reads another's tables, and sqlc generates each module's queries against its own service's migrations only, so a query that reads another service's table does not compile.
- Existing modules move as they are: `internal/application/item` becomes `services/item/internal/item`, with the same layers inside (handler, service, repository). Layout, not a rewrite.
- The Makefile and CI build and test each service on its own (`go test ./services/item/...`) and all of them together.

Three things the plan's tree did not have, because Go's `internal/` rule needs them (decision 170): `internal/sim` (gamedata's map geometry is the simulation's), `internal/progression` (the world's tick runs the character rules), and the generated code under `api/gen/<service>/v1` rather than `api/gen/internal/...`, which only `api/gen` itself could import (decision 169).

## Internal transport

- **gRPC.** Unary calls for commands and queries. A server stream from a service to the gateway for "deliver to character X" events: the world's and social's `Deliver`, and account's `WatchSessions`. One more stream feeds the world: social's `WatchParties`.
- **The client protocol stays JSON** in Stage 1 (page 4); only the gateway speaks it.
- **NATS** replaces the delivery streams in Stage 2, when there is more than one gateway node. **Kafka** is only for after-commit domain events through the outbox (Stage 2), never live traffic.
- **Versioned and backward-compatible.** Every contract is a versioned proto package (`internal.item.v1`). Fields are added, never renumbered or retyped, so old and new nodes run side by side during a rolling deploy. `buf lint` runs with `make lint`, and `buf breaking` runs in CI from step 28, once every contract exists (decision 186).
- **Routing keys.** Every internal message carries the keys that route it and make a retry safe: the character ID, the map ID for world commands, the connection ID for a join and a leave, and the idempotency key for anything that changes items. The request ID rides in gRPC metadata (`x-request-id`) for the logs.
- **Identity.** The gateway authenticates once (the token on REST, Hello on the WebSocket) and forwards the caller's identity as fields of the request: `account_id`, `character_id`. Internal services trust it on the internal network in Stage 1; mTLS comes later.
- **Insecure transport inside the Compose network** in Stage 1, the same trust boundary.

### Errors across gRPC

R1's model stays (decision 168): services return plain sentinel errors named for the failure, and the gateway owns the mapping to what a client sees. `pkg/errs` stays deleted.

1. Each service's proto declares `enum Reason` with one value per sentinel its clients can meet (`REASON_PARTY_FULL`, `REASON_INVENTORY_FULL`).
2. Each service's gRPC handler has a table from its sentinels to a gRPC code and a `Reason`. A failure leaves as a status whose details carry `google.rpc.ErrorInfo`, with the reason's name and the proto package as its domain. An error the table does not know leaves as `Internal`, its text kept in the service's log, never sent.
3. `pkg/grpcx` reads the reason back on the calling side, and tells a rule (a reason is present) from a transient failure (none), which is what the world's pickup retry needs (`item.IsRule` today).
4. The gateway's `errmap` maps each (domain, reason) to the client's own code, HTTP status and player message, as it maps sentinels today. The codes, `testdata/errors/codes.json`, and every word a player reads stay the same.
5. Guard tests close the loop: in each service, every exported `Err` of its modules has a row; in the gateway, every value of every service's `Reason` enum has a row.

A service the gateway cannot reach answers `internal`, as a database failure does today (decision 181).

## Contracts

One proto package per service under `api/proto/internal/`. A request names its owner (`account_id`, `character_id`) and every response is its own message, so a field can be added later. The sketch, written in full in step 22:

**`internal.account.v1.AccountService`**

| RPC | Request | Response |
| --- | --- | --- |
| `Register` | username, password | account (id, username, created_at) |
| `Login` | username, password, ip | token |
| `Validate` | token | account_id, session_id |
| `Rotate` | session_id | token |
| `Check` | session_id | |
| `Revoke` | session_id | |
| `RevokeAll` | account_id | |
| `GetSettings` / `UpdateSettings` | account_id (and settings) | settings |
| `WatchSessions` (stream) | gateway_id | Subscribed (boot_id), then SessionRevoked (session_id) and AccountRevoked (account_id, reason: new login or logout) |

**`internal.character.v1.CharacterService`**

| RPC | Request | Response |
| --- | --- | --- |
| `Create` | account_id, name, class | character |
| `List` | account_id | characters |
| `Load` | character_id, account_id (ownership) | character, with max HP and MP |
| `SaveCheckpoint` | character_id, map_id, x, y, hp, mp, exp | |
| `SaveLevelUp` | character_id, level, exp, stats, hp, mp | |

**`internal.item.v1.ItemService`**

| RPC | Request | Response |
| --- | --- | --- |
| `Inventory` | character_id | items |
| `Add` | actor (account_id, character_id), idempotency_key (the drop ID for a pickup), template_id, quantity, cause | changed slots |
| `Move` | actor, idempotency_key, item_id, tab, slot | changed slots |
| `Discard` | actor, idempotency_key, item_id, quantity | changed slots |
| `GrantStarter` | actor, class; the character ID is the idempotency key | changed slots |

**`internal.world.v1.WorldService`**

| RPC | Request | Response |
| --- | --- | --- |
| `Deliver` (stream) | gateway_id | Subscribed (boot_id), then one message per event for a player who joined through this gateway: character_id, tick, and one of Snapshot, Delta, Teleport, Damage, Died, Respawned, ExpGained, LevelUp, Chat, InventoryChanged, PickupFailed (with the item service's reason) |
| `Join` | gateway_id, connection_id, account_id, character_id | the character as the player sees it (stats, HP and MP with their maximums, map, position) |
| `Leave` | character_id, map_id, connection_id | |
| `Input` | character_id, map_id, seq, keys | |
| `Attack`, `PickUp`, `EnterPortal` | character_id, map_id | |
| `Say` | character_id, map_id, sender name, text (already cleaned by social) | |

**`internal.social.v1.SocialService`**

| RPC | Request | Response |
| --- | --- | --- |
| `Deliver` (stream) | gateway_id | Subscribed (boot_id), then PartyState, PartyInvited and party chat lines, each for one character |
| `Online` / `Offline` | gateway_id and the member (id, name, level, HP, max HP, map) / character_id | |
| `PartyOf` | character_id | the party, if any (Welcome) |
| `Invite`, `Respond`, `LeaveParty`, `Kick`, `Promote` | the caller's character_id and the command's fields | |
| `Chat` | sender id and name, channel, text | the cleaned line; a map line is the caller's to hand to the world, a party line is already delivered |
| `WatchParties` (stream) | world_id | Subscribed (boot_id), every party's membership, then each change |
| `UpdateMember` | character_id, map, level, HP, max HP (from the world) | |

The gateway serves no gRPC.

## Flows

### Hello

1. The gateway's per-player queue runs Hello (the read loop only decodes and routes). Version check as today.
2. `account.Validate`, then `account.Rotate`: the token in Welcome is the rotated one (step 16).
3. `world.Join(gateway_id, connection_id, account_id, character_id)`. The world loads the character from the character service (ownership checked there: someone else's character is `not found`, and the gateway kicks `unauthenticated` as today), asks the item service for the starter items (idempotent, keyed by the character ID: a no-op unless character creation crashed before granting them), and joins the map instance, which sends the Snapshot through the delivery stream. The reply is the character as the player sees it.
4. `item.Inventory` and `social.PartyOf`, then Welcome. Deliveries that reach the gateway before Welcome is sent (the Snapshot, a first Delta) wait in the session and follow Welcome in order (decision 176).
5. `social.Online` with the gateway's ID, so party traffic finds this gateway.

A failure after the join leaves the world (`world.Leave`) before the connection closes. A newer connection of the same account still kicks the older one with `logged_in_elsewhere`; the older one's `Leave` carries its own connection ID, so it can never remove the newer presence (decision 178).

### Commands

- The read loop decodes, applies the per-account rate limits, and puts the message on the player's command queue. It never blocks on a service. A full queue answers `rate_limited` when the message has a request ID, as an over-limit command does.
- One goroutine per player drains the queue in order, with a context that ends when the connection closes. Input, Attack, PickUp and EnterPortal become unary world calls carrying the character and the map; inventory commands call the item service; party commands call social; ChatSend asks social, and a map line goes on to `world.Say`.
- Inputs are unary calls, as the plan says (decision 183): ordered by the queue and by the world's sequence numbers. A client-streamed input channel is the Stage 2 option if the call rate shows in the tick p99.
- Ping never leaves the gateway.

### Deliveries

- At start, the gateway opens `world.Deliver`, `social.Deliver` and `account.WatchSessions` with its own ID (random per process) and waits for each Subscribed.
- The world's sink for a player is the gateway's stream plus the character ID. `Send` never blocks: it queues on the stream. The queue is bounded; if it fills, the world closes that stream and the gateway rejoins its players (decision 180). A slow player is still the gateway's to close (4001), as today.
- The gateway turns each delivery into the protocol message it was before (the translation in `sink.go` today, from proto instead of world types) and enqueues it on the player's connection.
- A Teleport moves the session to the target map, so later commands carry the new map ID.

### REST

The REST handler moves into the gateway as it is. It authenticates with `account.Validate`, then calls the account or character service with the caller's account ID: sign up, log in, log out, log out everywhere, characters, settings. The REST contract (`api/openapi.yaml`) does not change.

### Character creation and starter items

No transaction spans two services (rule 1 below). The character service commits the character, then calls `item.GrantStarter` with the character ID as the idempotency key, retrying with backoff until it succeeds or the request's deadline ends (decision 177). The world calls it again on every join, as a safety net for a crash in between. Character creation answers success once the character is committed: a client that saw an error would retry and meet `name_taken`.

`Starter()` through `Within` stays a pattern for modules inside one service (page 1); character creation no longer uses it.

### Pickups, checkpoints and the tick

- **Pickups** already run on the world's economy workers: the worker calls the item service's `Add`, keyed by the drop ID, and retries once when the failure carries no reason. The result returns through the instance's inbox, as today.
- **Checkpoints and level-ups** go to the character service from the persistence worker, which retries a failed save with backoff, in order, for up to a minute before logging it (decision 182).
- **Parties.** The world keeps a local copy of party membership, fed by social's `WatchParties` stream; loot windows and EXP sharing read the copy. When a member's map, level or HP changes, the world queues `social.UpdateMember` for a worker; the tick never waits for it.

### Party and chat

- Party commands go from the gateway to social with the caller's character ID; social pushes PartyState and PartyInvited to each member through its delivery stream, to the gateway that holds that member (it learns which from `Online`).
- A party chat line is delivered by social to every member, on any map. A map line comes back to the gateway, which hands it to the world (`Say`), and the map instance sends it to everyone on the map with the next tick's events, as today.

## Restarts and failures

| What restarts | What players see | How |
| --- | --- | --- |
| world | A short freeze, then play resumes at the last checkpoint; sockets stay up | The gateway's `Deliver` stream breaks. It keeps every socket, re-subscribes with backoff, and calls `world.Join` again for each connected player; the world reloads them from the character service. A player whose saved map differs from the one they were on gets MapChange, then the Snapshot. Mobs and ground drops reset, as after a crash today. |
| social | Parties are gone (in memory, as after a crash today); sockets stay up | A new boot ID on Subscribed tells the gateway the parties are gone: each player gets PartyState with no party, and the gateway registers its players with `Online` again. The world's `WatchParties` copy is rebuilt from the new, empty table. |
| account, character, item | Calls fail while it is down (the client sees `internal` for a command, or a Hello fails and the client retries) | gRPC clients reconnect on their own. Session ends missed during an account restart are caught by the gateway's minute check. |
| gateway | Each client reconnects | Rolling, one node at a time, draining first: readiness turns 503, every connection gets Kicked `server_restart` (the client reconnects with jitter), then the listener closes. |
| Postgres | As today | |

After a world restart the HUD's EXP can show more than the reloaded checkpoint until the next EXP event: Snapshot carries no EXP, and a fresh Welcome on the same socket would be a protocol change (open question 2).

## Rules that change because of the split

1. **No transaction spans two services.** Modules that must commit together live in the same service. Character creation and starter items: see above.
2. **The tick never waits on another service.** Party membership is a local copy; pickups run on workers; checkpoints go from the persistence worker; member updates go from a queue.
3. **Services never touch sockets.** Everything bound for a player goes through the sink, which now publishes to the gateway's delivery stream.
4. **Every internal message carries its routing key:** character ID, map ID, connection ID, request ID, idempotency key. A retry after a reconnect or a service restart must be safe.

## Rules that stay

- Layers inside each service (handler, service, repository); transactions through each module store's `InTx` with a unit of work; repositories never take a transaction.
- Generic code stays generic: non-domain logic goes in `pkg/`, written as if another project will reuse it.
- Backend steps 18 (one Input per tick) and 19 (EXP after death, invulnerability) still apply, in the world service.

## Migrations

- Each service with tables embeds its own goose files: account (`citext`, `accounts`, `sessions`, the rotation columns), character (`citext`, `characters`), item (`items`, `item_events`, `outbox`, `idempotency_keys`). `citext` is created by both account and character with `IF NOT EXISTS`, and neither drops it.
- `cmd/migrate` applies every service's set, each with its own version table (`goose_account_version`, `goose_character_version`, `goose_item_version`).
- A database migrated before the split (it has goose's default `goose_db_version`) is refused with a message saying to recreate it (`make down`); Stage 1 has no data worth carrying over (decision 184).
- The tests of a module apply only its own service's migrations, so a test that reads another service's table fails.

## Binaries, configuration and deployment

- **Binaries:** `services/<service>/cmd/<service>` for the six services; `cmd/migrate` and `cmd/bot` stay. `cmd/gameserver` is gone after step 28.
- **Composition roots:** each in `services/<service>/cmd/<service>/app` (R5's rule), which the end-to-end tests import to build the whole game in one test process.
- **Listeners:** the gateway serves `/api`, `/ws`, `/healthz`, `/readyz` and `/metrics` on `server.addr` (:8080). Every other service serves gRPC on its `listen` address and `/healthz`, `/readyz` and `/metrics` on its `ops` address, so Prometheus scrapes every process and the bot reads the tick percentiles from the world (decision 174). `pkg/runner` runs a binary's listeners and background tasks with readiness and graceful shutdown (today's `app.Server`, made generic).
- **Configuration:** one schema (`internal/config`) and one `configs/config.yaml` for every binary, each reading the sections it needs (decision 173). A new `services` section gives each internal service its `listen`, `addr` (where callers dial it) and `ops` addresses:

```yaml
services:            # Stage 1: one node each; Compose sets addr to the service's name
  account:   { listen: ":9001", addr: "localhost:9001", ops: ":9101" }
  character: { listen: ":9002", addr: "localhost:9002", ops: ":9102" }
  item:      { listen: ":9003", addr: "localhost:9003", ops: ":9103" }
  world:     { listen: ":9004", addr: "localhost:9004", ops: ":9104" }
  social:    { listen: ":9005", addr: "localhost:9005", ops: ":9105" }
```

- **Compose:** Postgres, `migrate`, and the six services, each built from the one Dockerfile with `SERVICE` set; the gateway publishes :8080 and the world its ops port for the bots. Builds share Go's caches through BuildKit cache mounts.
- **Make:** `up`, `down`, `test`, `cover`, `lint` and `smoke` keep their meaning for the whole game; `make test-service SERVICE=item` tests one service; `make run` starts the migrations and the six binaries without Docker; `make gen` runs sqlc and `buf generate`.
- **CI:** the go job runs lint (golangci-lint and `buf lint`), every test and coverage, and checks the generated code is current; a matrix job tests each service on its own; the compose job runs `make up` and the smoke test against the gateway; `buf breaking` against the previous commit from step 28.
- **Tools:** `buf`, `protoc-gen-go` and `protoc-gen-go-grpc` are pinned as Go tools in `server/go.mod` (`go get -tool`), so a laptop and CI generate with the same versions (decision 175).

## Testing

- Module tests move with their modules and keep passing as they are: rules with fakes, repositories on real Postgres.
- Each gRPC handler is tested over an in-memory connection (`bufconn`) with a fake service: proto in, the right call, proto or the right status and reason out, with the guard test over its error table.
- Each client adapter is tested against a fake gRPC server the same way.
- The gateway's tests run against fake services served over `bufconn`, so they exercise the real clients.
- The end-to-end tests in `server/test/` build every service in one test process from their composition roots, on loopback listeners, with the gateway served by `httptest`. The rows keep their names.
- New end-to-end rows: the world restarts while two players are connected, both sockets stay open and both see a Snapshot again; social restarts and each player gets PartyState with no party.
- The crash test runs every service as its own process, kills the world and the item service without warning in the middle of pickups, restarts them, and checks the item invariants and each hunter's inventory.

## Steps

| Step | Builds | Commit type |
| --- | --- | --- |
| [20. Shared kernel](./backend-steps/20-shared-kernel.md) | `pkg/typedid`, `pkg/tiled`; `internal/gamedata`, `internal/sim`, `internal/progression` moved out of the application modules | refactor |
| [21. Migrations per service](./backend-steps/21-migrations-per-service.md) | each service's goose files and version table; `cmd/migrate` applies them all; sqlc per service | refactor |
| [22. gRPC foundation](./backend-steps/22-grpc-foundation.md) | the five contracts, `buf` and the generators as Go tools, `pkg/grpcx`, `pkg/runner`, `pkg/hub` | build |
| [23. Item service](./backend-steps/23-item-service.md) | `services/item`; pickups, inventory commands and starter items over gRPC | feat |
| [24. Account service](./backend-steps/24-account-service.md) | `services/account`; sessions over gRPC, kicks through `WatchSessions` | feat |
| [25. Social service](./backend-steps/25-social-service.md) | `services/social`; parties and chat over gRPC, its delivery stream, the world's party copy | feat |
| [26. Character service](./backend-steps/26-character-service.md) | `services/character`; REST, Hello and checkpoints over gRPC | feat |
| [27. World service](./backend-steps/27-world-service.md) | `services/world`; the delivery stream, Hello through the world, rejoin on a world restart | feat |
| [28. Gateway service](./backend-steps/28-gateway-service.md) | `services/gateway`; per-player command queues, the drain, Compose and CI per service, `buf breaking` | feat |

Services leave the game server in dependency order, the ones that call nothing first, so the whole game builds, tests and runs after every step (decision 185): item, account and social call nothing; character calls item; the world calls three of them; what remains of the game server becomes the gateway. Until its consumer leaves too, the gRPC adapter for an extracted service sits in the game server's composition root.

### Existing steps that change

The step pages say what they built and stay as they were; each gets a note naming the step that changed it.

| Step | What changes |
| --- | --- |
| 01. Foundations | The ID types sit on `pkg/typedid` (20). |
| 02. Database foundation | Migrations per service, each with its own version table (21). |
| 04. Game data | `internal/gamedata` on `pkg/tiled`; the simulation moves to `internal/sim` (20). |
| 05. Accounts and sessions | The account service; kicks travel through `WatchSessions` (24). |
| 06. Characters and stats | The character service; starter items after the commit; the rules move to `internal/progression` (20, 23, 26). |
| 07. REST API and server wiring | REST in the gateway, forwarding to account and character (24, 26, 28). |
| 09. World | The world service; checkpoints through the character service (26, 27). |
| 10. Gateway | The gateway service: Hello through the world, per-player command queues, delivery streams (27, 28). |
| 12. Items and inventory | The item service; `GrantStarter` is its own idempotent command (23). |
| 13. Drops and pickup | Pickups call the item service's `Add`; the crash test kills the world and the item service (23, 27). |
| 15. Chat and party | The social service; the world's party copy (25). |
| 16. Session hardening | Kicks from the account service through `WatchSessions` (24). |
| 17. Packaging | Compose runs every service; the Dockerfile's `SERVICE` argument; the bot reads the world's metrics (23 to 28). |
| 18, 19 | Unchanged, in the world service. |

## Where the plan did not match the repository

The plan was written against Notion and the Stage 1 tree. These defaults follow the user's own changes of 2026-10-23; each is a numbered decision in `docs/ai-coding-log.md`.

1. **Docs:** Notion is no longer updated (R6). This page and the step pages are in `docs/tech/lld/`, and each step edits the pages it changes (decision 166).
2. **Layout:** the module is `server/` and Compose is in `infra/` (R2). The plan's tree goes inside `server/`; the Dockerfile stays in `server/` with a `SERVICE` argument (decision 167).
3. **`pkg/errs`:** R1 deleted it. Services keep plain sentinels; reasons cross gRPC in `ErrorInfo`; the gateway maps them (decision 168).
4. **`pkg/typedid` and `pkg/tiled`** are new, extracted from `internal/model/id.go` and the game-data loader (step 20).
5. **Go's `internal/` rule** adds `internal/sim` and `internal/progression`, and puts the generated code at `api/gen/<service>/v1` (decisions 169, 170).
6. **Shared tables:** `outbox` and `idempotency_keys` were shared on page 2; only the item module uses them, so the item service owns them. A later service that needs an outbox gets its own table.

## Open questions

Each has a default the steps build; the user can change it.

1. **An unreachable service answers `internal`.** A dedicated `unavailable` code would tell the player to wait, but adds a client-visible code. Default: `internal`, no protocol change.
2. **EXP after a world restart.** The player resumes at the checkpoint, but the HUD keeps the EXP it had until the next EXP event. A Welcome on the same socket would refresh it, which the client does not expect today. Default: leave it; the freeze is short and the next kill corrects it.
3. **Inputs as unary calls.** 20 calls a second per player. Default: unary, as the plan says; revisit with the load test.
4. **Databases migrated before the split** are refused rather than adopted. Default: recreate (`make down`).
5. **A social restart forgets parties**, as a crash did before. Persisting parties is TD-19's question.
