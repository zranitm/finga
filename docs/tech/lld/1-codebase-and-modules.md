# 1. Codebase and modules

## Repository

The layout and tooling follow [zranitm/docviewer](https://github.com/zranitm/docviewer), so both repositories read the same way (TD-20).

```txt
server/                 the Go module: everything the server builds, reads and publishes
  api/
    openapi.yaml        REST contract: accounts, sessions, characters, settings
    asyncapi.yaml       WebSocket message catalogue
    proto/internal/     the internal gRPC contracts, one package per service: internal/<service>/v1 (page 10)
    gen/                their generated Go code, api/gen/<service>/v1, committed; make gen regenerates it
  cmd/                  one binary per job; each wires its own dependencies next to its main
    gameserver/         REST, WebSocket, map instances, background jobs
      app/              its composition root: Build wires every module; Server listens, runs the tasks, shuts down
    migrate/            applies every service's goose migrations, then exits
      app/              the sets in order and the guard against a database migrated before the split; the end-to-end tests use it
    auditor/            Stage 2: checks the invariants continuously, as a Kafka consumer
    outbox-relay/       Stage 2
    bot/                test players for the proof session (make bots); Stage 2 grows it into the load and chaos client
  configs/
    config.yaml         committed defaults; config.local.yaml is git-ignored
  data/                 game content: classes, stats, mobs, items, EXP curve, Tiled maps; the client generates from it
  internal/
    application/        business logic: the game's rules, one package per module
      account/  character/  world/  combat/  item/
      party/  chat/
      economy/  market/  audit/        Stage 2
    handler/
      api/              REST handlers, DTOs, middleware
      ws/               the gateway: WebSocket sessions, routing, fan-out
      errmap/           the error table: each service error's status, own code and player message
      router.go         the route table: /api, /ws, /metrics, /healthz, /readyz
    protocol/           WebSocket message types, registered with the codec
    model/              value types shared by every module and handler: IDs (on pkg/typedid), Class, Coins
    gamedata/           game data: classes, stats, mobs, items, the EXP curve and the maps (on pkg/tiled), loaded and validated at startup
    sim/                the movement simulation, shared with the client through testdata/movement
    progression/        derived stats, EXP awards, the death penalty: pure rules the character module and the world's tick both run
    config/             the configuration schema
    telemetry/          metrics and logging; implements the application's observer interfaces
  services/
    account/  character/  item/
      migrations/       each service's goose SQL files, embedded, with its own version table (step 21)
  pkg/                  generic libraries with no game knowledge
  test/                 end-to-end tests: one row per acceptance criterion
  testdata/             the contracts the client's tests check too
    movement/           movement vectors shared by Go and TypeScript
    contracts/          golden REST and WebSocket messages
    errors/             codes.json: every error code with its status and message, written from errmap
  Dockerfile            one file for every binary; the CMD build arg picks which
  .env.example  .golangci.yml  buf.yaml  buf.gen.yaml  go.mod  go.sum  sqlc.yaml
web/                    the client: Phaser 4, React, Vite, TypeScript; layout on page 7
infra/                  how it runs
  docker-compose.yml    make up / make down; builds ../server
  .env.example          the host ports Compose publishes (infra/.env is git-ignored)
  grafana/              dashboards (Stage 2); infrastructure as code joins here too
docs/                   the design (product/, tech/: these pages), ai-coding-log.md, proof-session.md
tools/                  developer tools; empty in Stage 1
.github/workflows/      CI: the go, node and compose jobs
.gitattributes  .gitignore  Makefile  README.md
```

- The repository is `finga`, in three parts: `server/`, `web/` and `infra/` (split on 2026-10-23 at the user's request). The server is one Go module, in `server/`, also named `finga` (TD-15). Paths on these pages that start with `api/`, `cmd/`, `configs/`, `data/`, `internal/`, `pkg/`, `services/`, `test/` or `testdata/` are under `server/`.
- The server is the reference for what the two sides share: the client generates its game data from `server/data/` and its tests check `server/testdata/`. The root `Makefile` drives all three parts.
- `cmd/*/main.go` only loads configuration, builds the binary, and runs it until SIGINT or SIGTERM. Each binary wires its own dependencies next to its main, its Composition Root (one per process, as close to the entry point as possible). `migrate` and `bot` wire in `main.go`; the game server wires in `cmd/gameserver/app`, a package of its own because Go cannot import a `main` package and the end-to-end tests build the server in process exactly as production does. Stage 2 binaries follow the same rule (moved from `internal/server` on 2026-10-23 at the user's request).
- Additions with no docviewer counterpart: `data/` (game content), `services/*/migrations/` (Postgres, one set per service), `internal/application`, which keeps business logic apart from transport and infrastructure, and `internal/handler/router.go`, because there are two handlers (REST and WebSocket) to mount behind one router.
- Each acceptance criterion in Features becomes a named row in `test/`, for example `F3/two-buyers-one-listing`.

## Dependency rules

- Imports flow one way: `cmd` → `internal/handler` → `internal/application` → the shared kernel (`internal/model`, `internal/gamedata`, `internal/sim`, `internal/progression`) → `pkg`. Never the other way round: nothing in `internal` or `pkg` imports `cmd`, and the shared kernel imports no module, handler, protocol, telemetry or configuration (moved out of the modules on 2026-10-27, step 20, so every service of page 10 can import it).
- `internal/application` holds only business logic. It never imports `handler`, `protocol`, `cmd`, `telemetry`, or `config`:
  - It returns its own result types, for example `world.Delta`, and the gateway turns them into protocol messages.
  - It reports metrics through small observer interfaces it declares, and `internal/telemetry` implements them.
  - It takes plain option structs, and the binary's composition root (`cmd/gameserver/app`) fills them from configuration.
- `pkg` imports only the standard library and infrastructure libraries (pgx, websocket, uuid). It never imports `internal`.
- When a module needs another module, it declares a small interface for exactly what it needs (Go's consumer-side interfaces). The composition root wires in the real implementation. This keeps modules free of import cycles and easy to test with fakes. When it needs the other module inside its own transaction, that interface hangs off its unit of work instead (see Rules at the boundaries).
- No module reads or writes another module's tables or sqlc queries.
- No global state. Everything comes in through constructors.
- Enforced in CI by golangci-lint's `depguard` rules, so a wrong import fails `make lint`.

## Layers and responsibilities

Every request passes through three layers, and each has one job. Nothing skips a layer: a handler never calls a repository, and a repository never makes a business decision.

<table>
<tr>
<th></th>
<th>Handler</th>
<th>Application (service)</th>
<th>Repository</th>
</tr>
<tr>
<th>Lives in</th>
<td><code>internal/handler/api</code> and <code>internal/handler/ws</code></td>
<td><code>internal/application/&lt;module&gt;/service.go</code></td>
<td><code>internal/application/&lt;module&gt;/repository.go</code> and <code>queries/</code></td>
</tr>
<tr>
<th>Job</th>
<td>Speak the transport: REST or WebSocket</td>
<td>Run a use case: the game's rules</td>
<td>Store and load the module's own tables</td>
</tr>
<tr>
<th>Does</th>
<td>Decodes input and rejects malformed requests (wrong types, missing fields, oversized bodies). Establishes the caller's identity from the token or the connection. Applies rate limits. Calls one service method. Turns the result and errors into HTTP responses or protocol messages.</td>
<td>Enforces business rules and invariants. Decides who may do what (ownership, party leader). Decides where a transaction starts and ends, by calling its store's <code>InTx</code>. Applies idempotency. Writes outbox events. Calls other modules through interfaces it declares.</td>
<td>Runs the module's queries on the connection it was built with. Locks rows when the service asks. Maps rows to domain types. The module's store (<code>Postgres</code>) opens transactions for <code>InTx</code>, builds the unit of work, and classifies database errors into the module's errors.</td>
</tr>
<tr>
<th>Never</th>
<td>Business rules, SQL, transactions, or calling another handler.</td>
<td>HTTP, WebSocket, JSON, protocol types, SQL text, a transaction handle, database errors, or reading configuration.</td>
<td>Business decisions, a transaction in a method signature, or touching another module's tables.</td>
</tr>
<tr>
<th>Types it uses</th>
<td>DTOs and <code>internal/protocol</code> messages</td>
<td><code>internal/model</code> and the module's own types in <code>api.go</code></td>
<td>sqlc rows, which never leave the repository</td>
</tr>
<tr>
<th>Tested with</th>
<td>A fake service, <code>httptest</code>, a fake connection</td>
<td>A fake store whose <code>InTx</code> hands over a fake unit of work, for rules; real Postgres through <code>pkg/pgtest</code> for whole flows</td>
<td>Real Postgres through <code>pkg/pgtest</code>, through the store so errors arrive classified; <code>classify</code> also gets a table test</td>
</tr>
</table>

**Rules at the boundaries**

- **Validation by meaning.** The handler rejects input that is malformed whatever the rules are. The service enforces rules that must hold for every caller, such as username format, password policy, or not discarding more than a stack holds. A future admin tool or test calling the service gets the same rules.
- **The service decides the transaction but never holds one.** Each module's store (`Postgres`, in `repository.go`) has `InTx(ctx, fn func(uow UnitOfWork) error)`. It opens the transaction with `pkg/dbx`, builds a unit of work whose repositories are bound to that transaction, runs `fn` with it, and classifies the error. The service declares `Store` and `UnitOfWork` in `deps.go`. Repository methods take only domain arguments.
- **A write outside a transaction doesn't compile.** Writes exist only on the unit of work. Plain reads sit on `Store` and run on the pool, with no transaction.
- **Another module joins the transaction through the unit of work.** Character creation's unit of work has `Starter()`: the item rules, bound to the same transaction. `cmd/gameserver/app` builds it from the item service's `Within(uow)` and the item store's `UnitOfWork(tx)`, so neither module imports the other.
- **The transaction function may run twice.** `InTx` runs all of it again on a deadlock or serialization failure, so it sends nothing and changes nothing in memory. Events leave through the outbox.
- **Ownership is decided in the service and made atomic in the repository.** The service passes the actor; the repository method says so in its name and puts it in the `WHERE` clause, for example `LockOwned(ctx, itemID, owner)`.
- **Database errors change shape once, at the module's boundary.** Repository methods return them as they are. The module's `classify` runs where its work hands a result back (`InTx`, plain reads, and work joined to another module's transaction) and turns them into the module's errors: `pgx.ErrNoRows` into `ErrNotFound`, a constraint violation into its domain error. Module errors are plain sentinel errors named for the failure (`ErrNotFound`, `ErrSlotTaken`, `ErrInventoryFull`), declared in `api.go`; `classify` wraps the database error behind one with `%w` for the logs, and the service returns them directly for rule violations. They carry no code and no player text: the handlers map each one to an HTTP status, a code of its own (`item_not_found`, `slot_taken`, `inventory_full`) and the player message through `internal/handler/errmap` (see Protocol, Error codes). No error's text ever reaches a client.
- **classify matches constraints by name and leaves the rest alone.** By name, so a unique violation from the items table, raised inside character creation's transaction, never becomes `ErrNameTaken`. Unknown errors pass through unchanged: retries still see `40001` and `40P01`, and the handler reports the rest as `internal`.
- **A rule that branches on absence gets a boolean.** For "add to the stack if there is one, otherwise insert", the repository method returns `(Stack, bool, error)`, so the service never checks a database error.
- **Real-time code is application code too.** Map instances live in `internal/application/world`. They reach the database only through services, from worker goroutines, never inside a tick.
- **sqlc's generated code is private.** It generates into `internal/application/<module>/internal/db`, which Go lets only that module import.

**One use case through the three layers: discarding an item**

```go
// handler/ws: transport only
func (h *Handler) onDiscard(ctx context.Context, s Session, m protocol.InventoryDiscard) protocol.Response {
    err := h.items.Discard(ctx, s.CharacterID, item.DiscardInput{
        ItemID: m.ItemID, Quantity: m.Quantity, Key: m.IdempotencyKey,
    })
    return responseFor(m.ReqID, err) // errmap: the error's own code and player message
}

// application/item/deps.go: what the service sees
type Store interface {
    InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
    Find(ctx context.Context, id model.ItemID, owner model.CharacterID) (Item, error) // on the pool
}

type UnitOfWork interface { // everything bound to one transaction
    Items() Repository // the module's own aggregate is just Repository (item.ItemRepository would stutter, and revive rejects it)
    Events() EventRepository
    Outbox() outbox.Writer
    Idempotency() Idempotency // pkg/idempotency, plus hashing the input and storing the response
}

// application/item/service.go: rules, and where the transaction starts
func (s *Service) Discard(ctx context.Context, actor model.CharacterID, in DiscardInput) error {
    return s.store.InTx(ctx, func(uow UnitOfWork) error {
        return uow.Idempotency().Run(ctx, actor, "discard", in.Key, in, func() error {
            it, err := uow.Items().LockOwned(ctx, in.ItemID, actor)
            if err != nil {
                return err // no row, also when it isn't yours: ErrNotFound after classify
            }
            if in.Quantity < 1 || in.Quantity > it.Quantity {
                return ErrBadQuantity // a sentinel from api.go; errmap answers it bad_quantity
            }
            if err := uow.Items().Reduce(ctx, it.ID, in.Quantity); err != nil {
                return err
            }
            if err := uow.Events().Append(ctx, discarded(it, in.Quantity, actor)); err != nil {
                return err
            }
            return uow.Outbox().Add(ctx, itemDiscarded(it, in.Quantity))
        })
    })
}

// application/item/repository.go: the store, its unit of work, classify, and SQL
func (p *Postgres) InTx(ctx context.Context, fn func(uow UnitOfWork) error) error {
    err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
        return fn(p.newUnitOfWork(tx))
    })
    return classify(err)
}

func (p *Postgres) newUnitOfWork(tx pgx.Tx) *unitOfWork {
    q := db.New(tx)
    return &unitOfWork{
        items:  itemRepo{q: q},
        events: eventRepo{q: q},
        outbox: outbox.NewWriter(tx),
        idem:   newIdempotency(idempotency.NewStore(tx)),
    }
}

// classify turns database errors into this module's errors. Unknown ones pass
// through: retries still see 40001, and handlers report the rest as internal.
func classify(err error) error {
    var pgErr *pgconn.PgError
    switch {
    case errors.Is(err, pgx.ErrNoRows):
        return ErrNotFound
    case errors.As(err, &pgErr) && pgErr.ConstraintName == "one_item_per_slot":
        return ErrSlotTaken
    default:
        return err
    }
}

// SQL only, on whatever connection the repository was built with
func (r itemRepo) LockOwned(ctx context.Context, id model.ItemID, owner model.CharacterID) (Item, error) {
    row, err := r.q.LockOwnedItem(ctx, id, owner) // ... WHERE id = $1 AND owner_id = $2 FOR UPDATE
    if err != nil {
        return Item{}, err
    }
    return toItem(row), nil
}
```

## Module anatomy

```txt
internal/application/market/
  api.go          the module's public types, inputs, and errors
  service.go      application layer: use cases, rules, where transactions start
  deps.go         interfaces the service needs: its Store and UnitOfWork, other modules, observers
  repository.go   repository layer: the Postgres store (InTx, unit of work, classify) and the queries
  queries/*.sql   sqlc input
  internal/db/    sqlc output; only this module can import it
  events.go       domain events this module emits
  *_test.go
```

## Modules

Every module below lives in `internal/application/`, except the two handlers. The shared kernel beside them (`internal/gamedata`, `internal/sim`, `internal/progression`, `internal/model`) holds no state of its own: game data is static and read-only, the simulation and the progression rules are pure functions.

| Module | Owns | Uses |
| --- | --- | --- |
| `account` | Accounts, sessions, account settings | `pkg/auth`, `pkg/ratelimit` |
| `character` | Characters, stats, levels, EXP | gamedata, progression |
| `world` | Map instances, movement, mobs, ground drops, portals | character, combat, item, economy, gamedata, sim, progression, `pkg/tick` |
| `combat` | Hit and damage resolution, EXP awards; pure functions | gamedata |
| `item` | Item instances, inventory, item events | gamedata, `pkg/outbox`, `pkg/idempotency` |
| `economy` (Stage 2) | Wallets on the ledger | `pkg/ledger`, `pkg/outbox` |
| `market` (Stage 2) | Listings, purchase settlement, expiry | item, economy, `pkg/idempotency`, `pkg/outbox` |
| `party` | Parties and invites, in memory | A notifier interface |
| `chat` | Message validation and routing | world, party |
| `handler/ws` (the gateway) | WebSocket sessions, routing, fan-out to clients | `pkg/wsconn`, `pkg/auth`, `pkg/ratelimit`, every gameplay module |
| `handler/api` | REST endpoints: DTOs, validation, error responses through `handler/errmap` | account, character, `pkg/auth`, `pkg/ratelimit` |
| `audit` (Stage 2) | Invariant checks, read-only | item, economy, market |

## Generic libraries in /pkg

None of these knows anything about the game; each could ship in another project unchanged. Libraries that touch the database are built over a connection (`dbx.DBTX`) and never take a transaction as an argument, like the repositories: a module's unit of work builds them over its transaction.

```go
// pkg/dbx: transactions, under every module's store
type DBTX interface { // *pgxpool.Pool and pgx.Tx both satisfy it, as with sqlc's
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
type Beginner interface { // *pgxpool.Pool satisfies it; BeginTx so InTx can name READ COMMITTED
    BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}
// InTx commits fn's work; an error or a panic rolls back. A deadlock or
// serialization failure runs the whole of fn again, up to 3 times.
// Options (OnRetry, MaxAttempts) let a store count retries for its metrics.
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error, opts ...Option) error

// pkg/ledger (Stage 2): double-entry ledger over plain string account IDs
type Posting struct {
    Account string
    Amount  int64 // signed; a transfer's postings must sum to zero
}
type Transfer struct {
    ID       uuid.UUID
    Kind     string
    Postings []Posting
    Meta     map[string]string
}
type Ledger interface {
    OpenAccount(ctx context.Context, id string, allowNegative bool) error
    Post(ctx context.Context, t Transfer) error // ErrUnbalanced, ErrInsufficientBalance
    Balance(ctx context.Context, id string) (int64, error)
}
func New(db dbx.DBTX) Ledger

// pkg/outbox: transactional outbox
type Event struct {
    ID          uuid.UUID
    Topic, Key  string
    Type        string
    ContentType string
    Payload     []byte
    OccurredAt  time.Time
}
type Writer interface {
    Add(ctx context.Context, events ...Event) error
}
func NewWriter(db dbx.DBTX) Writer
// Stage 2 adds Relay, which publishes unsent rows through a Publisher interface.

// pkg/idempotency: run a command at most once per key
type Store interface {
    Do(ctx context.Context, owner, scope, key string, requestHash []byte,
        fn func() ([]byte, error)) ([]byte, error) // ErrKeyReused
}
func NewStore(db dbx.DBTX) Store

// pkg/ratelimit: keyed token buckets
type Limiter interface {
    Allow(key string, cost float64) bool
}

// pkg/auth: authentication behind an interface (TD-17)
type Principal struct{ AccountID, SessionID uuid.UUID }
type Authenticator interface {
    Verify(ctx context.Context, username, password string) (accountID uuid.UUID, err error)
}
type Sessions interface {
    Issue(ctx context.Context, accountID uuid.UUID) (token string, err error) // revokes older sessions
    Validate(ctx context.Context, token string) (Principal, error)
    Revoke(ctx context.Context, sessionID uuid.UUID) error
    SessionLifecycle // step 16: Rotate(ctx, sessionID) (token, error), Check(ctx, sessionID) error, RevokeAll(ctx, accountID) error
}

// pkg/tick: fixed-timestep loop with overrun metrics
type Steppable interface {
    Step(tick uint64, dt time.Duration)
}
func Run(ctx context.Context, interval time.Duration, s Steppable, opts ...Option) error

// pkg/wsconn: WebSocket sessions with pluggable encoding
type Message struct {
    Type  string
    Seq   uint32 // client input sequence, optional
    ReqID string // request/response correlation, optional
    Tick  uint64 // server tick, optional
    Data  any    // concrete message struct, resolved through a type registry
}
type Codec interface {
    Encode(Message) ([]byte, error)
    Decode([]byte) (Message, error)
}
type Conn interface {
    ID() string
    Send(Message) error // non-blocking; a full send queue closes the connection
    Close(code int, reason string)
}
type Handler interface {
    OnOpen(Conn)
    OnMessage(Conn, Message)
    OnClose(Conn, error)
}
```

```go
// pkg/typedid: typed UUIDv7 identifiers, one Go type per kind (step 20)
type ID[K any] uuid.UUID
func New[K any]() ID[K]
func Parse[K any](s string) (ID[K], error) // ErrMalformed
// model.CharacterID is typedid.ID[characterKind]: an item ID where a character ID is expected does not compile.

// pkg/tiled: Tiled's JSON map format, with no game's layer rules (step 20)
func Load(path string) (*Map, error) // errors name the file
func (m *Map) ObjectGroups() map[string]Layer
func (ps Properties) String(name string) (string, bool)
func (ps Properties) Int(name string) (int, bool)
```

```go
// pkg/grpcx: gRPC servers and clients every service shares (step 22)
type ErrorRow struct { Err error; Code codes.Code; Reason protoreflect.Enum } // a sentinel, its code, its proto reason
type ErrorTable []ErrorRow
func NewServer(opts ServerOptions, extra ...grpc.ServerOption) *grpc.Server // request ID in, panic as Internal, errors through the table
func Dial(addr string, extra ...grpc.DialOption) (*grpc.ClientConn, error)   // insecure in Stage 1; sends the request ID
func ReasonOf(err error) (Reason, bool) // the ErrorInfo a failed call carries: the proto package and the value's name
func IsRule(err error) bool             // a reason is present: retrying the same request cannot help
func Follow[T any](ctx context.Context, open func(context.Context) (grpc.ServerStreamingClient[T], error), handle func(*T), opts FollowOptions)
// grpcx/grpctest.Conn serves a *grpc.Server over bufconn for a test.

// pkg/runner: one process's listeners and tasks (step 22)
type Listener struct { Name, Addr string; Server Server } // runner.HTTP(handler) or runner.GRPC(server)
func New(opts Options, listeners []Listener, tasks []Task) *Runner
func (r *Runner) Run(ctx context.Context) error // ready while listening; on shutdown: not ready, Drain, listeners stop within the grace, tasks stop
func Probes(ready func() bool, metrics http.Handler) http.Handler // /healthz, /readyz, /metrics

// pkg/hub: subscribers with bounded queues, behind the delivery streams (step 22)
func New[T any](queue int) *Hub[T]
func (h *Hub[T]) Subscribe(id string) *Sub[T] // replaces and ends an older one with the same ID
func (s *Sub[T]) Send(v T) bool               // never blocks; a full queue ends the subscription
```

Also in `/pkg`: `configload` (copied from docviewer, see Configuration), `observability` (structured logging and metrics setup; TD-21), and `pgtest` (a fresh Postgres database per test, imported by tests only).

## Configuration

The same scheme as docviewer, using its `pkg/configload`:

- **Defaults** live in `configs/config.yaml`, committed and never secret. A git-ignored `configs/config.local.yaml` replaces it when present, or `CONFIG_PATH` points at another file.
- **Overrides:** any key can be set from the environment as `FINGA_` plus its path in upper case, for example `FINGA_SERVER_ADDR=:9090` or `FINGA_WORLD_TICK_RATE_HZ=30`.
- **`.env`:** the binaries never read it; they read the YAML file and their environment, nothing else. Whoever starts them injects the git-ignored `server/.env`: `make up` through Compose's `env_file`, where the Compose file's own `environment` entries (the database URL, the CORS origins) win over it, and `make run` by exporting it before starting the binaries without Docker. `server/.env.example` lists every override, commented out. The host ports Compose publishes are Compose's own settings, read from `infra/.env` (changed 2026-10-23 at the user's request: environment injection belongs to Docker and the tooling, not the code).
- **Validated at start:** the schema is `internal/config`. An unknown key or an invalid value stops the binary with a message naming the key.
- `gameserver` and `migrate` share the schema, and each reads the sections it needs. Stage 2 binaries get their own file, for example `configs/outbox-relay.yaml` and `configs/auditor.yaml`.

Starting `configs/config.yaml`:

```yaml
server:
  addr: ":8080"
  cors_origins: []          # browser origins allowed for REST and WebSocket; empty: same origin only
  shutdown_grace: 10s
database:
  url: postgres://finga:finga@localhost:5432/finga?sslmode=disable
  max_conns: 20
world:
  tick_rate_hz: 20
  checkpoint_interval: 30s  # TD-02
  data_dir: data
economy:
  workers: 8
telemetry:
  log_level: info
services:            # the internal services (page 10): gRPC listener, where callers dial, ops listener
  account:   { listen: ":9001", addr: "localhost:9001", ops: ":9101" }
  character: { listen: ":9002", addr: "localhost:9002", ops: ":9102" }
  item:      { listen: ":9003", addr: "localhost:9003", ops: ":9103" }
  world:     { listen: ":9004", addr: "localhost:9004", ops: ":9104" }
  social:    { listen: ":9005", addr: "localhost:9005", ops: ":9105" }
```

## Tooling and conventions

Carried over from docviewer:

- **`make` targets:** `up` and `down` (Docker Compose), `run` (every service's migrations and the game server without Docker, with `server/.env` exported), `test` (Go with `-race`, plus the web tests), `cover` (fails below 80% of statements in `internal/`, `pkg/` and `cmd/gameserver/app/`), `lint` (golangci-lint, plus ESLint and Prettier for the web), and `gen` (sqlc, and `buf generate` for the gRPC code, with `buf`, `protoc-gen-go` and `protoc-gen-go-grpc` pinned as Go tools in `go.mod`), `smoke` (against a running stack: a client signs up, enters the world and moves; CI runs it after `make up`), and `bots` (the proof session: four bots play for 30 minutes and the tick p99 is printed).
- **`.golangci.yml`:** the same linters, including `errorlint`, `revive`, and `sloglint` with constant log messages, attributes as key-value pairs, no global logger, and snake_case keys.
- **Tests:** table-driven, with named cases. No test logic in production code: tests use the seams production already has (interfaces, constructors, `httptest`) and `testing/synctest` for time.
- **`docs/ai-coding-log.md`:** one row per implementation step: what was produced, how it was verified, what changed, mistakes caught, and the commit.
