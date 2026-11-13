# One binary: a modular monolith

> **Status: approved · 2026-11-13.** The high-level design for merging the six services into one process, with their boundaries kept as modules. It describes the code as it stands on `main` (after #68) and the change it plans. It starts after C12, when no feature branch is open.

## 1. Why

- **At this size the split costs more than it gives:**
  - It would isolate failures only for item and social, and only with degraded modes that don't exist.
  - A world restart loses its in-memory state whether or not the gateway keeps the socket.
  - The services already ship together: one SHA for every image, and mixed versions unsupported. Independent deploys go unused.
- **One process removes** network failures between modules, the gRPC plumbing, and five deploy units with their ports.
- **The module boundaries stay,** so a later split remains possible. Sharding the world across nodes is the one case where a separate gateway pays off.

## 2. What has to hold

- **A module's internals stay private.** Another module reaches it only through its facade, and the rule tests enforce it.
- **The tick never waits on the database, and the read loop never blocks.** The `itemcalls` pool and its batching, world's party copy, and the gateway's per-player command queue stay as they are.
- **A bug in one module doesn't take the game down.** A panic inside a call to another module becomes an error, as a gRPC call's did.
- **Every goroutine runs under `pkg/runner` or `panics.Recorder.Go`, or recovers its own panic,** so no panic goes unrecovered. A rule test enforces it over every package the binaries link:
  - the `go` keyword appears only in `pkg/panics` and `pkg/runner`;
  - every other call that starts a goroutine (`errgroup`'s and a `WaitGroup`'s `Go`, `context.AfterFunc`, `time.AfterFunc`) is a reviewed site, listed by function in the test. A new one has to be looked at before it joins the list.
  - Main had three bare `go` statements before step 1: the gateway's `go in.commands()` (`receive/inbox.go`), world's persistence goroutine (`world/world.go`), and `pkg/grpcx/server.go`'s drain hook.
  - In one process, any of them panicking would kill the game.
- **Streams keep their rules:** bounded queues, a publisher that never blocks, and a full subscriber closed, then resubscribed and resynced.
- **What players see doesn't change:** the client protocol, REST, `/ws`, and every error code stay as they are.
- **The database doesn't change:** one Postgres, each module's own migrations and goose table, and no module reading another's tables.
- **Each step is a PR with CI green,** and its docs change in the same PR.

## 3. Where it stands

- **Services:** `server/services/{gateway,account,character,item,world,social}`, each with:
  - a 20-line `main.go` calling `host.Main` (in `server/cmd/<service>` since step 2, so the service's root package can be its facade);
  - `app/` (the composition root, which the end-to-end tests reuse);
  - `config/`;
  - `internal/`;
  - migrations, for account, character and item.
- **Contracts:** one gRPC service per module, in `api/proto/internal/<svc>/v1`. Every host also serves gRPC health.
- **Who calls whom:**
  - The gateway calls account, character, item, world and social. Its `internal/clients` is about 1,130 lines without tests, with `delivery.go` mapping the streams.
  - Character calls item's `GrantStarter`.
  - World calls character's `Load` and `SaveCheckpoint`, item through the `itemcalls` pool, and social's `UpdateMember` and `Announce`. Its `internal/clients` is about 520 lines without tests.
- **Streams:**
  - world `Deliver`, social `Deliver` and account `WatchSessions` go to the gateway;
  - social `WatchParties` goes to world.
  - Each producer is already a `pkg/hub` keyed by subscriber, and `grpcx.ServeSubscription` sends `Subscribed{BootId}` first.
  - Consumers use `grpcx.Follow`, which resubscribes with backoff from 100 ms to 5 s and calls `OnBreak` so the consumer resyncs.
- **Host** (`internal/host`):
  - `New`, `Module`, `Dial`, `Serve`, `GameData`, `Registry`, `OnDrain`, `OnClose`, `Run` and `Close`, plus the generic `Main` and `Start`.
  - `h.Module(name)` gives a module what is counted under its name: `Task`, `Database`, `ServeGRPC`, `Calls` (its facade's calls, from step 2), `Registry` and `TraceRoots`.
  - Each process has its own Prometheus registry, panic counter, gRPC metrics and tracer provider.
- **Ports:**
  - public 8080, on the gateway;
  - ops 9100 to 9105;
  - gRPC 9001 to 9005.
- **Config:**
  - A strict per-binary YAML with unprefixed keys (`ops.addr`, `log`, `tracing`, `database.url`, `data_dir`, `grpc.listen`).
  - Environment overrides are `FINGA_<PATH>`.
  - Four services load the game data separately.
- **Tests:**
  - `test/e2e_test.go` builds every service's `app` in one test process, over loopback gRPC.
  - `test/process_e2e_test.go` builds the six binaries and kills and restarts world or item alone.
- **Tools:**
  - `cmd/migrate` applies the three migration sets;
  - `cmd/bot` is a WebSocket client that reads world's and item's metrics on 9104 and 9103;
  - `cmd/testers` dials account, character and item over gRPC.
- **Infra:**
  - Compose runs the six services, postgres, migrate, nginx and testers.
  - The Dockerfile builds one image per binary.
  - `deploy.yml` has a matrix of seven images (the six services and migrate).
  - `deploy.sh` pulls everything, optionally stops everything, then brings it up. It has no order between services.
- **Monitoring:**
  - Alloy has one scrape job, `finga`, with a `service` label per target.
  - There is one dashboard with 24 panels. Three of them group by `service`, and the logs panel lists the six names.
  - There are no alert rules.

## 4. Modules and facades

### Layout

- Each service directory becomes a module. Its `internal/` stays private, and its root package becomes the **facade**: `services/item/item.go` and so on.
  - Go lets the facade import its own `internal/`, and nothing else can.
  - Until step 6 deletes them, the per-service mains live in `server/cmd/<service>`, since a directory holds one package.
- **The facade exports:**
  - `New(ctx, Deps) (*Module, error)`, which builds the module from its `host.Module` and adds its tasks and drains there, so there is no separate `Register`;
  - methods mirroring today's RPCs;
  - `Subscribe` for a stream.
- **Its types are its contract.** The facade declares Go types for its requests, results and events, in place of the internal protos. Identifiers, errors and anything already shared stay in `internal/kernel`.
  - The facade converts between them and the module's model, as the gRPC handler converts proto today, so the internals can change without changing the contract.
- **Errors are `kernel` values.** Each facade has an error table from its model's errors to kernel errors, in place of the handler's gRPC table.
  - A `kernel.Rule` is a failure no retry passes, one per contract reason, in `internal/kernel/errors_<module>.go`. Its snake-case reason is unique across modules, and `kernel.ReasonOf` reads it.
  - `pkg/transient.ErrUnavailable` stays the failure a later try may clear, and `kernel.ErrInternal` is a recovered panic.
  - A facade wraps both errors, `fmt.Errorf("%w: %w", kernel.ErrX, cause)`, so `errors.Is` finds the kernel error and the logs keep the cause.
  - The gateway's `PublicOf` stays, so every client-facing code is unchanged.
- **Dependency order:**
  1. account and item, which call nobody;
  2. character, which calls item;
  3. social;
  4. world, which calls character, item and social;
  5. the gateway, which calls them all.

  The graph has no cycles, and the rule tests keep it so.

### Every facade call

A small helper in the facade wraps each method:

- **Span:** a trace span named `<module>.<Method>`, with a `module` attribute. It replaces the gRPC interceptor's span, so traces read as they do today.
- **Metrics:** `finga_module_calls_total{module,method,code}` and `finga_module_call_duration_seconds{module,method}`. They replace `grpc_server_*` and `grpc_client_*` on the dashboard.
- **Panics:** a panic is recovered, counted in `finga_panics_total{module}`, logged with its stack, and returned as `kernel.ErrInternal`. A bug in item fails that call, not the game.
- **Deadlines:** the caller's context carries the deadline, as it does now.

### Adapters

- Each consumer's `internal/clients` keeps its ports (`internal/application/ports.go`) and gains an in-process adapter over the other module's facade.
- During steps 2 to 5 both the gRPC adapter and the in-process one exist, and the composition root picks one. Step 6 deletes the gRPC ones.
- Application code barely changes, since it only ever saw the ports.

### Streams

- **A producer's facade has `Subscribe(id) (*hub.Subscription[Event], error)`** over the hub it already has, with the same bounded queue: world 8192, social 4096, sessions 1024.
- **A new `hub.Follow` replaces `grpcx.Follow` for in-process consumers.** It subscribes, pumps events to the handler, and on a close calls `OnBreak` so the consumer resyncs.
  - It resubscribes after a short backoff: 10 ms to 1 s, since no network sits between them.
  - A panic in the handler breaks the subscription and is counted, as now.
- **`BootId` goes.** One process has one boot, and a break always means a resync.
- **Kept and tested:** the publisher never blocks, a full subscriber is closed, and the consumer resubscribes and resyncs.

## 5. One host

- **One process, one host:**
  - one Prometheus registry;
  - one panic counter, labelled by module;
  - the Go and process collectors once;
  - one tracer provider (`service.name` is `finga`), with `otel.SetErrorHandler` called once;
  - one logger, each module's carrying `module`.
- **Listeners:**
  - public HTTP (REST and `/ws`) on 8080;
  - ops (health, readiness and `/metrics`) on 9100;
  - and, in dev and CI only, the admin API (section 8).
- **The host's gRPC server, health service and `Dial` go in step 6.**
- **Database:**
  - One pgx pool for the modules that have tables (account, character and item), from `host.Database` once.
  - Transaction metrics carry the module whose store ran the transaction: `finga_db_tx_duration_seconds{module}` and `finga_db_tx_retries_total{module}`.
- **Game data loads once,** and the modules that need it share the read-only copy.

### Draining on SIGTERM

1. Readiness goes off.
2. The gateway stops accepting, sends every player `Kicked{server_restart}`, and closes the sockets.
3. World flushes its checkpoints and its pending item batches.
4. Every module's tasks stop.
5. The database pool closes, then the tracer flushes.

A second signal kills at once, as now. The whole drain runs within `shutdown_grace`.

### When a goroutine fails

Today a panicked map stops `World.Run` and only the world process restarts; a failed item or social task restarts only that service. In one process the host decides, by what the goroutine owns:

- **A call** (a facade method): recovered and returned as `kernel.ErrInternal` (section 4).
- **A connection or a request:**
  - the gateway's per-player command goroutine closes that player's socket, so they reconnect;
  - an HTTP handler answers 500.
  - The game goes on for everyone else.
- **A restartable task,** a loop that keeps no live game state of its own:
  - Examples:
    - item's outbox and retention;
    - account's session watch and retention;
    - social's sweeps and retention;
    - the gateway's stream pumps;
    - world's party copy.
  - The host restarts it with backoff, 100 ms to 5 s.
  - It counts `finga_task_restarts_total{module,task}` and logs the panic with its stack.
  - Streams resync through `OnBreak`, as after any break.
- **A vital task,** one that owns live game state: world's map tick loops, its persistence loop, and its `itemcalls` workers, which hold players' pending items.
  - A failure drains the process as on SIGTERM. The other maps flush their checkpoints and pending batches, and players are kicked with `server_restart`.
  - Then the process exits non-zero, and Compose's restart policy starts it again.
  - That's what a world restart does today, except that the sockets close too and players reconnect.
- **Each task says which it is** when it's registered (`h.Task(name, run, host.Restartable)` or `host.Vital`). There's no default, so every new task chooses.

## 6. Config

One file, strict as now. Its shared sections are followed by one section per module:

```yaml
http: {addr: ":8080", cors_origins: [...]}
ops: {addr: ":9100"}
admin: {addr: "127.0.0.1:9200", enabled: false}
shutdown_grace: ...
log: {...}
tracing: {otlp_endpoint: ...}
database: {url: ...}
data_dir: data
gateway: {ws: {...}, sessions: {...}}
account: {...}
character: {...}
item: {...}
world: {...}
social: {...}
```

- **Overrides** stay `FINGA_<PATH>`, now including the module, as in `FINGA_WORLD_<KEY>`.
- **Dropped:** `grpc.listen` and `dependencies.*`.

## 7. Monitoring

- **Metric names keep their `finga_*` names.**
- **The scrape's `service` label becomes `service="finga"` on one target.** A new `module` label sits on the metrics that are shared across modules: panics, database transactions, retention, outbox, and module calls.
- **Dashboard:**
  - The three panels that grouped by `service` group by `module`.
  - The gRPC panels become module-call panels.
  - The logs panel selects `{service="finga"}` and splits by the `module` attribute.
- **There are no alert rules to move.**
- **`cmd/bot`'s metric flags** default to 9100.

## 8. Tools

- **`cmd/migrate` stays.** It already applies the three sets under one lock, each with its own goose table.
- **`cmd/testers` uses a private admin API:**
  - HTTP on its own address, off by default.
  - Dev Compose and CI turn it on. Prod never does, and it's enforced as #67 enforces `production.invalid`:
    - the prod overlay pins `admin.enabled` to false;
    - CI renders the prod Compose config and fails if the admin API is on.
    - It can make sessions and grant items, so it must never reach prod.
  - It listens on its own address, never the public or ops listener, so neither nginx nor a scrape can reach it.
  - Its endpoints are the calls testers makes today (sessions, characters, inventory and item grants), written through the modules' facades. The item and idempotency rules therefore hold.
  - Testers keeps driving the gateway for the rest.
- **`cmd/bot` doesn't change,** beyond the metrics port.

## 9. Infra and CI

- **Image:** one `finga` image from `cmd/finga`, plus the `migrate` image. The Dockerfile's `SERVICE` argument goes.
- **Compose, dev and prod:** one `finga` service (public 8080, ops 9100, and admin in dev) in place of six. nginx routes to it. Monitoring scrapes it.
- **`deploy.yml`:** the image matrix is `[finga, migrate]`.
- **`deploy.sh`:** pulls, stops if asked, starts one service, then runs the same health and URL checks.
- **Makefile:**
  - `run` starts one process.
  - `test-service` goes, and `make test` covers everything.
  - `breaking` runs on the client contract only, after step 6.
  - Every other target keeps its name.
- **CI:**
  - The per-service matrix goes.
  - The generated-code check covers only the client protos once the internal ones are gone.
  - Lint, `cover-go` (80%), the web job, the Compose job (smoke, testers and edge) and the session-link check stay.

## 10. Tests

- **End to end:** `newStack` builds `cmd/finga`'s app in the test process, with real Postgres from `pgtest`. A service restart in a test becomes a whole-app restart.
- **Process tests:** they build the `finga` binary.
  - A `kill -9` and restart brings players back by reconnecting, from the last checkpoint.
  - An item failure during world's batches is tested by fault injection: a test-built item facade that fails a set number of calls. It's assembled in the test's composition, not as test-only API.
- **Rule tests** (`test/rules`):
  - **layers:** unchanged inside a module.
  - **`TestNoServiceReachesAnother` becomes `TestModulesMeetAtFacades`:** a module imports another only at its root package, never its `internal/`, and the dependency graph has no cycles.
  - **contracts:** goes, with the internal protos.
  - **error tables:** every model error has a row in its facade's table, and every kernel error a gateway `PublicOf` row.
  - **no unrecovered goroutine:** a `go` statement appears only in `pkg/panics` and `pkg/runner`, and every other goroutine a binary starts is a reviewed site (section 2).
  - **ctx names, stamping, timestamps and docs:** unchanged.
- **Unit tests** move with their code. Each facade gets table-driven tests of its conversion and error table.

## 11. When things fail

| Failure | What happens | Why it is safe |
| --- | --- | --- |
| A module panics during a call | That call fails with an internal error; the panic is counted and logged | The facade recovers it, as the gRPC server did |
| A stream consumer falls behind | Its subscription is closed; it resubscribes and resyncs | The hub never blocks the publisher, as now |
| The process crashes | Every player is disconnected and reconnects; world restarts from its last checkpoints | The client already reconnects after a restart; checkpoints are as frequent as now |
| A deploy | Every player gets `Kicked{server_restart}`, then reconnects | The drain kicks players before world flushes |
| Postgres is down | Logins and item writes fail; the world keeps ticking and retries its saves | Unchanged: the tick never waits on the database |
| A restartable task panics | It restarts within 5 s; a stream it fed resyncs | The host restarts it with backoff and counts it |
| A vital task panics: a map's tick, world's persistence, or an `itemcalls` worker | Every player is kicked with `server_restart` and reconnects to a fresh process | The process drains, flushing what it can, and exits; Compose restarts it from checkpoints, as a world restart does today |
| A connection's goroutine panics | That player's socket closes and they reconnect | It's recovered per connection; nobody else notices |

## 12. Build order

Seven PRs, each green on `make test`, `make lint`, `make cover`, `make breaking` and the Compose job, with their docs in the same PR:

1. **One host for many modules:**
   - a shared registry and `module` labels, one panic counter, and database metrics per pool;
   - the merged config;
   - game data loaded once;
   - restartable and vital tasks, the three bare `go` statements moved under the runner, and the rule test banning them.

   Every service still runs alone as today.

   As built:
   - `host.Start` builds the process's host from the merged config, and each binary's `Build` adds its module with `h.Module(name)`: its logger carries `module`, and its panics, tasks and transactions are counted under its name.
   - One file, `server/config/config.yaml`: the shared sections, then a section per module. Until step 6, a module's section also holds its process keys (`ops`, `grpc`, `dependencies`), so an override names the module, as in `FINGA_WORLD_DEPENDENCIES_ITEM`. The top-level `ops` comes with `cmd/finga`.
   - The shared families (panics, transactions, retention, the outbox gauge and task restarts) register once per registry, or join the family already there.
   - Tasks are restartable or vital as section 5 lists them. World runs as three vital tasks: the map loops (`world`), `saves`, and `item calls`.
     - `saves` retries a failed save until the maps have stopped. Then it retries each checkpoint left until it lands, within `flush_timeout`, which follows `shutdown_grace` as before.
     - A vital task's failure drains the process as SIGTERM does. So when `item calls` fails, the maps keep ticking through the drain window, until the tasks are stopped.
     - Social's sweeps run on timers, not as a task, each under the module's recorder.
   - The rule test covers every way to start a goroutine (section 2), called or taken as a function value, in a function or a package-level initialiser. The reviewed sites recover on their own goroutine: the wsconn reader, writer, pinger and last flush close the socket with 1011, and the gateway's command goroutine closes its player's socket.
2. **Facades for account, item, social and character,** with the call helper (span, metrics and panic recovery). Each gRPC handler becomes a thin layer over its facade.

   As built:
   - The six mains moved to `server/cmd/<service>`. The goroutine rule names the six game binaries and fails on any other binary in `cmd/` that isn't a listed tool (bot, migrate, testers).
   - `internal/facade` is the call helper, from `host.Module.Calls(errors)`. A method starts with `ctx, call := m.calls.Start(ctx, "Name")` and defers `call.End(&err)`.
     - The span is `<module>.<Method>`, with a `module` attribute.
     - The metrics are `finga_module_calls_total{module,method,code}` and `finga_module_call_duration_seconds{module,method}`, shared families. The code is `ok`, the rule's reason, `unavailable`, `canceled` (the caller gave up) or `internal`, and nothing else.
     - A panic is recorded by the module's recorder under the flow `call` and returns `kernel.ErrInternal`.
   - `ErrUnknownClass` became a rule. `ErrBadID` stays `typedid.ErrMalformed`, so an ID decoded from JSON still matches it, and `ReasonOf` reads it as `bad_id`.
   - Each handler converts proto to and from its facade's types. Its table maps kernel errors to the same codes and reasons as before, so the wire is unchanged. A reasonless `codes.Internal` row answers with the text a recovered panic always has.
   - Item's facade checks idempotency keys, the cause of an Add, and an AddMany's size, refusing a malformed drop alone, so callers in the same process meet the same rules. The handler keeps its checks in their old order.
   - Account's and social's notifiers, the handlers' streams, stay a `Deps` field until step 3 brings `Subscribe`. Character takes item's starter grant the same way until step 4.
   - Rule tests:
     - every model error has a row in its facade's table, and each of the module's kernel rules and contract reasons one in its handler's;
     - every `NewRule` call is in kernel, with a constant snake-case reason that no other rule and no call code takes;
     - the gateway's `PublicOf` covers kernel's shared errors, and the module rules get rows in step 4, when the gateway first calls a facade.
   - `kernel.IsRule` comes with its first caller in step 4. Each caller's retry predicate carries over unchanged there: `!IsRule` for character's starter grant and world's loot, transient for world's saves and social calls.
3. **World's facade and in-process streams:** `Subscribe` and `hub.Follow`.
4. **`cmd/finga` and the in-process adapters:**
   - one composition root;
   - the end-to-end stack and the process tests on it;
   - fault injection for item.

   The six binaries still build.
5. **Infra:**
   - the image and Compose, dev and prod;
   - `deploy.yml` and `deploy.sh`;
   - Alloy and the dashboard;
   - the Makefile and CI;
   - the admin API, with testers moved onto it, pinned off in the prod overlay and checked in CI;
   - the bot's port.

   Smoke, bots and testers pass.
6. **Delete gRPC:**
   - the internal protos and their generated code;
   - the handlers, gRPC clients and error maps;
   - the per-service mains, configs and apps;
   - the host's gRPC server, health service and `Dial`;
   - `runner.GRPC`, and `pkg/grpcx` (its `Follow` moved to `hub.Follow` in step 3).

   The rule tests change as in section 10, and the root README and infra READMEs are updated.
7. **Rename `services/` to `modules/`:** mechanical, at a moment with no feature branch open.

## 13. Decisions

Agreed on 2026-11-13:

1. **Rename `services/` to `modules/`, last and alone.** Every import path changes, so it waits for a quiet moment.
2. **`cmd/testers` uses a private admin API** that writes through the modules. It's on in dev and CI, off in prod, and on its own address, never the ops port.
3. **Starter items stay an idempotent call,** not one transaction. A transaction across modules would tie one module's store to another's tables.
4. **Metric names stay.** The `service` label becomes a `module` label on the shared metrics, and three panels and the logs panel change.
5. **Facades recover panics,** so one module's bug fails a call, not the game.
6. **Facades add a span and call metrics** in place of gRPC's, so traces and the dashboard keep their detail.

Settled in review on 2026-11-13:

7. **A failed goroutine fails what it owns:** a call, a connection, or a task. Restartable tasks restart with backoff. Vital ones (world's map loops, persistence and `itemcalls` workers) drain and exit the process. No bare `go`.
8. **The admin API is pinned off in prod,** and CI checks the rendered prod config.

Also kept from the brief:
- the idempotent starter-items call and world's safety net;
- world's party copy;
- the `itemcalls` pool.

## 14. Alternatives considered

- **Keeping six services:** see section 1.
- **Merging without facades,** with modules importing each other's packages directly: simpler at first, but the boundaries would erode and a later split would be a rewrite.
- **Facade types as aliases of each module's model types:** no conversion code, but every internal change would become a contract change.
- **Testers writing to Postgres directly:** it would skip the item rules and idempotency, and it's safe only while the characters are offline.
- **Keeping gRPC between modules inside one process:** it keeps the plumbing, the error maps, and the conversion costs for no isolation.
