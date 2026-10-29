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
  cmd/                  the binaries that are not services; each wires its own dependencies next to its main
    migrate/            applies every service's goose migrations, then exits
      app/              the sets in order and the guard against a database migrated before the split; the end-to-end tests use it
      config/           its own configuration: config.yaml (the database, the log level) and its type
    auditor/            Stage 2: checks the invariants continuously, as a Kafka consumer
    outbox-relay/       Stage 2
    bot/                test players for the proof session (make bots); Stage 2 grows it into the load and chaos client
  data/                 game content: classes, stats, mobs, items, EXP curve, Tiled maps; the client generates from it
  internal/
    protocol/           WebSocket message types, registered with the codec: the gateway, the bot and the end-to-end tests
    kernel/             value types shared by every service: IDs (on pkg/typedid), Class, Coins (internal/model until step 30)
    gamedata/           game data: classes, stats, mobs, items, the EXP curve and the maps (on pkg/tiled), loaded and validated at startup
    sim/                the movement simulation, shared with the client through testdata/movement
    progression/        derived stats, EXP awards, the death penalty: pure rules the character module and the world's tick both run
    host/               how every service runs: its gRPC and ops listeners on pkg/runner, the binaries' shared main that loads each one's own configuration, and the sections every service's configuration has
    telemetry/          metrics and logging; implements the services' observer interfaces
  services/             one folder per service (page 10); each module lives in its service
    gateway/            the only public entry; no database, no game rules
      cmd/gateway/      main.go and app/: Build dials every service; Server listens, drains on SIGTERM, runs the tasks
      config/           its own configuration: config.yaml (http, the five dependencies) and its type
      internal/handler/
        api/            REST handlers, DTOs, middleware
        ws/             WebSocket sessions, the per-connection command queue, routing, the delivery streams
        errmap/         each service failure's reason to its status, own code and player message
        router.go       the route table: /api, /ws, /metrics, /healthz, /readyz
      internal/clients/ the account service as pkg/auth's Guard
    item/
      cmd/item/         main.go and app/, its composition root
      config/           its own configuration: config.yaml, with only its keys (page 11), and its type
      internal/model/   Item, Event, Cause, Slot, the errors, the outbox payloads (step 32)
      internal/application/  add, move, discard, the starter grant, the inventory; ports.go
      internal/repository/   the Postgres store, its unit of work, classify; queries/, internal/db/
      internal/handler/ the ItemService gRPC server and its error table
      invariants/       the item invariant checks, outside internal/ so the end-to-end tests and the auditor can run them
      migrations/       its goose SQL files, embedded, with its own version table (step 21)
    account/            in layers (step 31): cmd/account, config/, internal/model, internal/application, internal/repository, internal/handler (AccountService and WatchSessions), migrations/
    social/             config/; party/ and chat/ in internal/, its handler (SocialService, Deliver, WatchParties); no database
    character/          the same shape as item, and internal/clients/ for the item service (starter items)
    world/              internal/world/ and internal/combat/ (the map instances and the hit rules), internal/clients/ (character, item, the party copy), its handler (WorldService, Deliver); no database
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
- Every `main.go` only loads configuration, builds the binary, and runs it until SIGINT or SIGTERM. Each binary wires its own dependencies next to its main, its Composition Root (one per process, as close to the entry point as possible). `migrate` and `bot` wire in `main.go`; each service wires in `services/<name>/cmd/<name>/app`, a package of its own because Go cannot import a `main` package and the end-to-end tests build the whole game in process exactly as production does. Stage 2 binaries follow the same rule (moved from `internal/server` on 2026-10-23 at the user's request; the game server became the services of page 10 in steps 20 to 28).
- Additions with no docviewer counterpart: `data/` (game content), `services/` (one folder per service, each module inside its service, with its migrations in `services/*/migrations/`), and the gateway's `internal/handler/router.go`, because there are two handlers (REST and WebSocket) to mount behind one router.
- Each acceptance criterion in Features becomes a named row in `test/`, for example `F3/two-buyers-one-listing`.

## Dependency rules

- Imports flow one way: composition roots (`services/*/cmd/*/app`, `cmd/migrate/app`) → each service's handler and clients → its application → its model → the shared kernel (`internal/kernel`, `internal/gamedata`, `internal/sim`, `internal/progression`) → `pkg`; a service's repository implements its application's ports and imports application and model (page 11, Layers). Never the other way round: nothing in `internal`, `pkg` or a service's `internal/` imports a composition root, and the shared kernel imports no service, contract, protocol, telemetry, configuration or `internal/host` (moved out of the modules on 2026-10-27, step 20, so every service of page 10 can import it; `internal/model` became `internal/kernel` in step 30, so every service's own layer can be called `model`).
- A service never imports another: Go's `internal/` rule keeps it out of the other's internals, and depguard out of the rest (its migrations, its composition root). It reaches another only through the client generated from `api/proto/internal/` (page 10).
- A service's layers are packages (page 11; each service moves into them in steps 31 to 36, and until its step keeps them as files of one module package, `services/<name>/internal/<module>`): `model` imports none of the others; `application` imports `model`; `repository` imports `application` and `model`; `handler` and `clients` import `application` and `model`, never `repository` or each other. depguard has one rule per layer (`layer-model`, ..., `layer-clients`), naming every service's packages, since it matches a denied path by prefix with no wildcard; a new service is added to each list.
- The inner layers (`model`, `application`, `repository`, and a module not yet moved) hold only business logic and its storage. They never import the contracts (`api/gen`), gRPC, `protocol`, `telemetry`, a configuration package or `internal/host`:
  - It returns its own result types, for example `world.Delta`; its service's handler turns them into the contract's messages, and the gateway turns those into protocol messages.
  - It reports metrics through small observer interfaces it declares, and `internal/telemetry` implements them.
  - It takes plain option structs, and its service's composition root fills them from configuration.
- The gateway holds no database: no pgx, `pkg/dbx`, `pkg/outbox` or `pkg/idempotency` under `services/gateway` (step 28).
- `pkg` imports only the standard library and infrastructure libraries (pgx, websocket, uuid). It never imports `internal`.
- When a module needs another module, it declares a small interface for exactly what it needs (Go's consumer-side interfaces). The composition root wires in the real implementation. This keeps modules free of import cycles and easy to test with fakes. When it needs the other module inside its own transaction, that interface hangs off its unit of work instead (see Rules at the boundaries).
- No module reads or writes another module's tables or sqlc queries.
- No global state. Everything comes in through constructors.
- Enforced in CI by golangci-lint's `depguard` rules, so a wrong import fails `make lint`.

## Layers and responsibilities

Every request passes through three layers, and each has one job. Nothing skips a layer: a handler never calls a repository, and a repository never makes a business decision.

Each layer is a package of the service (page 11). The model below them holds the entities, value types and domain errors every layer uses. Steps 31 to 36 move each service into these packages; until its step, a service keeps the same layers as files of one module package (`internal/<module>/service.go`, `repository.go`, with the model in `api.go` and the ports in `deps.go`).

<table>
<tr>
<th></th>
<th>Handler</th>
<th>Application (service)</th>
<th>Repository</th>
</tr>
<tr>
<th>Lives in</th>
<td>Each service's <code>internal/handler</code> (gRPC); the gateway's <code>internal/handler/api</code> and <code>internal/handler/ws</code></td>
<td><code>services/&lt;name&gt;/internal/application</code>: the use cases, and in <code>ports.go</code> the interfaces they need</td>
<td><code>services/&lt;name&gt;/internal/repository</code>, with <code>queries/</code> and sqlc's <code>internal/db/</code></td>
</tr>
<tr>
<th>Job</th>
<td>Speak the transport: gRPC, or REST and WebSocket at the gateway</td>
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
<td><code>internal/kernel</code>, the service's <code>model</code>, and its own inputs and results</td>
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
- **The service decides the transaction but never holds one.** Each service's store (`Postgres`, in `repository/postgres.go`) has `InTx(ctx, fn func(uow application.UnitOfWork) error)`. It opens the transaction with `pkg/dbx`, builds a unit of work whose repositories are bound to that transaction, runs `fn` with it, and classifies the error. The application declares `Store` and `UnitOfWork` in `application/ports.go`. Repository methods take only domain arguments.
- **A write outside a transaction doesn't compile.** Writes exist only on the unit of work. Plain reads sit on `Store` and run on the pool, with no transaction.
- **Another module joins the transaction through the unit of work, inside one service.** A module that must commit with another of the same service gets that module's rules bound to its unit of work (a `Within(uow)` on the other service, wired by the composition root), so neither module imports the other. Across services there is no shared transaction (page 10): character creation commits the character, then asks the item service for the starter items, keyed by the character ID (changed 2026-10-27, step 23; until then character creation used this pattern).
- **The transaction function may run twice.** `InTx` runs all of it again on a deadlock or serialization failure, so it sends nothing and changes nothing in memory. Events leave through the outbox.
- **Ownership is decided in the service and made atomic in the repository.** The service passes the actor; the repository method says so in its name and puts it in the `WHERE` clause, for example `LockOwned(ctx, itemID, owner)`.
- **Database errors change shape once, at the module's boundary.** Repository methods return them as they are. The module's `classify` runs where its work hands a result back (`InTx`, plain reads, and work joined to another module's transaction) and turns them into the module's errors: `pgx.ErrNoRows` into `ErrNotFound`, a constraint violation into its domain error. A service's errors are plain sentinel errors named for the failure (`ErrNotFound`, `ErrSlotTaken`, `ErrInventoryFull`), declared in its `model`; `classify` wraps the database error behind one with `%w` for the logs, and the service returns them directly for rule violations. They carry no code and no player text: the service's gRPC handler sends each as a reason in its contract (`REASON_INVENTORY_FULL`), and the gateway maps the reason to an HTTP status, a code of its own (`item_not_found`, `slot_taken`, `inventory_full`) and the player message through its `errmap` (see Protocol, Error codes; page 10). No error's text ever reaches a client.
- **classify matches constraints by name and leaves the rest alone.** By name, so a unique violation from the items table, raised inside character creation's transaction, never becomes `ErrNameTaken`. Unknown errors pass through unchanged: retries still see `40001` and `40P01`, and the handler reports the rest as `internal`.
- **A rule that branches on absence gets a boolean.** For "add to the stack if there is one, otherwise insert", the repository method returns `(Stack, bool, error)`, so the service never checks a database error.
- **Real-time code is application code too.** Map instances live in the world service's application (`services/world/internal/world` until step 35). They reach the database only through services, from worker goroutines, never inside a tick.
- **sqlc's generated code is private.** It generates into `services/<name>/internal/repository/internal/db`, which Go lets only the repository import.

**One use case through the three layers: discarding an item**

```go
// services/item/internal/handler: transport only. The gateway sent the
// actor from the connection, never from the message, and maps a failure's
// reason to the client's code and player message (errmap).
func (s *Server) Discard(ctx context.Context, req *itemv1.DiscardRequest) (*itemv1.DiscardResponse, error) {
    actor, err := actorOf(req.GetActor())
    if err != nil {
        return nil, err
    }
    id, err := kernel.ParseItemID(req.GetItemId())
    if err != nil {
        return nil, err
    }
    res, err := s.svc.Discard(ctx, actor, application.DiscardInput{Key: req.GetIdempotencyKey(), ItemID: id, Quantity: int(req.GetQuantity())})
    if err != nil {
        return nil, err // the error table sends it with its reason
    }
    return &itemv1.DiscardResponse{Changed: slots(res)}, nil
}

// services/item/internal/application/ports.go: what the use cases see
type Store interface {
    InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
    Inventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) // on the pool
}

type UnitOfWork interface { // everything bound to one transaction
    Items() Repository // the module's own aggregate is just Repository (item.ItemRepository would stutter, and revive rejects it)
    Events() EventRepository
    Outbox() outbox.Writer
    Idempotency() Idempotency // pkg/idempotency, plus hashing the input and storing the response
}

// services/item/internal/application/service.go: rules, and where the transaction starts
func (s *Service) Discard(ctx context.Context, actor kernel.CharacterID, in DiscardInput) error {
    return s.store.InTx(ctx, func(uow UnitOfWork) error {
        return uow.Idempotency().Run(ctx, actor, "discard", in.Key, in, func() error {
            it, err := uow.Items().LockOwned(ctx, in.ItemID, actor)
            if err != nil {
                return err // no row, also when it isn't yours: ErrNotFound after classify
            }
            if in.Quantity < 1 || in.Quantity > it.Quantity {
                return model.ErrBadQuantity // a sentinel from the model; errmap answers it bad_quantity
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

// services/item/internal/repository/postgres.go: the store, its unit of work, classify, and SQL
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
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

// classify turns database errors into the model's errors. Unknown ones pass
// through: retries still see 40001, and handlers report the rest as internal.
func classify(err error) error {
    var pgErr *pgconn.PgError
    switch {
    case errors.Is(err, pgx.ErrNoRows):
        return model.ErrNotFound
    case errors.As(err, &pgErr) && pgErr.ConstraintName == "one_item_per_slot":
        return model.ErrSlotTaken
    default:
        return err
    }
}

// SQL only, on whatever connection the repository was built with
func (r itemRepo) LockOwned(ctx context.Context, id kernel.ItemID, owner kernel.CharacterID) (model.Item, error) {
    row, err := r.q.LockOwnedItem(ctx, id, owner) // ... WHERE id = $1 AND owner_id = $2 FOR UPDATE
    if err != nil {
        return model.Item{}, err
    }
    return toItem(row), nil
}
```

## Module anatomy

A service's layers are packages (page 11); a module is a set of files across them, named for it. The item service with the Stage 2 market module beside its items, which it shares transactions with (page 10):

```txt
services/item/internal/
  model/           item.go, market.go: entities, value types, the errors (ErrInventoryFull, ...); events.go: the domain events
  application/     item.go, market.go: the use cases, their inputs and results; ports.go: Store, UnitOfWork, repositories, other modules, observers
  repository/      postgres.go: the store (InTx, the unit of work, classify); items.go, listings.go: the queries' adapters
    queries/*.sql  sqlc input
    internal/db/   sqlc output; only the repository can import it
  handler/         the gRPC server and its error table
  *_test.go        beside the code they test
```

## Modules

Every module below lives in its service, `services/<name>/internal/<module>`; the gateway's two handlers hold no game rules. The shared kernel beside them (`internal/gamedata`, `internal/sim`, `internal/progression`, `internal/kernel`) holds no state of its own: game data is static and read-only, the simulation and the progression rules are pure functions.

| Module | Owns | Uses |
| --- | --- | --- |
| `account` (the account service, `services/account`) | Accounts, sessions, account settings | `pkg/auth`, `pkg/ratelimit` |
| `character` (the character service, `services/character`) | Characters, stats, levels, EXP | gamedata, progression, the item service (starter items) |
| `world` (the world service, `services/world`) | Map instances, movement, mobs, ground drops, portals | combat, the character service (load, checkpoints, level-ups), the item service (pickups, starter items), the social service (a party copy), gamedata, sim, progression, `pkg/tick` |
| `combat` (in the world service) | Hit and damage resolution, EXP awards; pure functions | gamedata |
| `item` (the item service, `services/item`) | Item instances, inventory, item events | gamedata, `pkg/outbox`, `pkg/idempotency` |
| `economy` (Stage 2, in the item service) | Wallets on the ledger | `pkg/ledger`, `pkg/outbox` |
| `market` (Stage 2, in the item service) | Listings, purchase settlement, expiry | item, economy, `pkg/idempotency`, `pkg/outbox` |
| `party` (the social service, `services/social`) | Parties and invites, in memory | A notifier interface (the social handler) |
| `chat` (the social service) | Message validation and routing | party |
| `handler/ws` (the gateway service) | WebSocket sessions, a command queue per connection, routing, the delivery streams, fan-out to clients | `pkg/wsconn`, `pkg/auth`, `pkg/ratelimit`, the services' generated clients |
| `handler/api` (the gateway service) | REST endpoints: DTOs, validation, error responses through `handler/errmap` | the account and character services' clients, `pkg/auth`, `pkg/ratelimit` |
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
    Guard
}
type Guard interface { // what the REST handler and the gateway need; implemented over the account service's client (step 24)
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
// kernel.CharacterID is typedid.ID[characterKind]: an item ID where a character ID is expected does not compile.

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
func Error(code codes.Code, reason protoreflect.Enum) error // a failure with a reason, for fakes and adapters
func Reasons(d protoreflect.EnumDescriptor) []Reason      // every value of a reason enum but the zero one
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

// pkg/sentinels: reads Go source for the sentinel errors a package declares (step 23),
// so each error table's guard test checks it maps every one
func Declared(dirs ...string) ([]string, error)
func Referenced(files ...string) (map[string]bool, error)
```

Also in `/pkg`: `configload` (copied from docviewer, see Configuration), `observability` (structured logging and metrics setup; TD-21), and `pgtest` (a fresh Postgres database per test, imported by tests only).

## Configuration

The same scheme as docviewer, using its `pkg/configload`:

- **One file per binary, inside it** (page 11, step 29): the defaults are `services/<name>/config/config.yaml` (`cmd/migrate/config/config.yaml` for migrate), committed and never secret, with only the keys that binary uses. A git-ignored `config.local.yaml` beside it replaces it when present, or `CONFIG_PATH` points at another file. The binaries run from `server/` (and `/app` in an image, which holds the binary's own file at the same path), so the paths are relative to it, as `data/` is.
- **Overrides:** any key can be set from the environment as `FINGA_` plus its path in upper case, read by each binary that has the key, for example `FINGA_DATABASE_URL`, `FINGA_DEPENDENCIES_ITEM=item:9003` or `FINGA_TICK_RATE_HZ=30`.
- **`.env`:** the binaries never read it; they read the YAML file and their environment, nothing else. Whoever starts them injects the git-ignored `server/.env`: `make up` through Compose's `env_file`, where the Compose file's own `environment` entries (each service's database and the addresses it dials) win over it, and `make run` by exporting it before starting the binaries without Docker. `server/.env.example` lists every override, commented out. The host ports Compose publishes are Compose's own settings, read from `infra/.env` (changed 2026-10-23 at the user's request: environment injection belongs to Docker and the tooling, not the code).
- **Validated at start:** each binary's type sits beside its file (`services/<name>/config`, package `config`), and `internal/host` loads it with the strict loader: an unknown key (another service's, too) or an invalid value stops the binary with a message naming the key and the file. The sections every service has are `internal/host`'s types (`GRPCSection`, `OpsSection`), and the database section is `pkg/dbx`'s `Config`. Stage 2 binaries get their own the same way (the outbox relay, the auditor).

The world service's `services/world/config/config.yaml`:

```yaml
grpc:
  listen: ":9004"
ops:
  addr: ":9104"            # /healthz, /readyz, /metrics
shutdown_grace: 10s
log_level: info
data_dir: data             # the game data, relative to server/
tick_rate_hz: 20
checkpoint_interval: 30s   # TD-02
economy:
  workers: 8               # the pool that runs pickups and inventory actions
dependencies:              # where this service dials the ones it calls
  character: localhost:9002
  item: localhost:9003
  social: localhost:9005
```

The other services' files have the same shape with their own keys (README, Configuration); the gateway's has `http` (`addr`, `cors_origins`) in place of `grpc` and `ops`.

## Tooling and conventions

Carried over from docviewer:

- **`make` targets:** `up` and `down` (Docker Compose), `run` (every service's migrations, then the six services without Docker, with `server/.env` exported), `test` (Go with `-race`, plus the web tests), `test-service SERVICE=<name>` (one service built and tested on its own, as CI's matrix does), `cover` (fails below 80% of statements in `internal/`, `pkg/`, `services/` and `cmd/migrate/app/`), `lint` (golangci-lint, plus ESLint and Prettier for the web), `breaking` (`buf breaking` against the previous commit), and `gen` (sqlc, and `buf generate` for the gRPC code, with `buf`, `protoc-gen-go` and `protoc-gen-go-grpc` pinned as Go tools in `go.mod`), `smoke` (against a running stack: a client signs up, enters the world and moves; CI runs it after `make up`), and `bots` (the proof session: four bots play for 30 minutes and the tick p99 is printed, read from the world's ops address).
- **`.golangci.yml`:** the same linters, including `errorlint`, `revive`, and `sloglint` with constant log messages, attributes as key-value pairs, no global logger, and snake_case keys.
- **Tests:** table-driven, with named cases. No test logic in production code: tests use the seams production already has (interfaces, constructors, `httptest`) and `testing/synctest` for time.
- **`docs/ai-coding-log.md`:** one row per implementation step: what was produced, how it was verified, what changed, mistakes caught, and the commit.
