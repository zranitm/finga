# 3. Game server runtime

## Process

One binary per job, as in docviewer. The game runs as six services (page 10), each a binary of its own, `services/<name>` (its `main.go` at the service's root):

- `gateway` is the only public entry: REST and the WebSocket gateway, with no database and no game rules (step 28).
- `world` runs the map instances, the persistence worker and the economy workers (step 27).
- `account`, `character`, `item` and `social` hold their modules and tables, served over gRPC.

And the tools:

- `cmd/migrate` applies every service's migrations, each set with its own version table, and exits.
- `cmd/bot` drives test players against a running game for the proof session and prints the tick percentiles from the world's `/metrics` (its ops address, `-metrics`).
- `cmd/auditor` (Stage 2) checks the invariants continuously, as a Kafka consumer.

The gateway starts one HTTP server and follows the services' streams. Routes: REST under `/api`, WebSocket at `/ws`, `/healthz` for liveness, `/readyz` for readiness, and metrics at `/metrics`. Every other service serves gRPC on its listen address and the same three probes on its ops address.

**Readiness** means this process can take traffic: `/readyz` is 200 from the moment it listens (game data loaded, everything wired) until shutdown starts, and 503 before and after, so a load balancer stops sending new connections while in-flight requests drain. On SIGTERM the gateway then drains: it refuses new WebSocket connections, tells every connection the server is restarting (Kicked `server_restart`; the client reconnects with jitter), and closes them before its listener shuts down, within the grace period (step 28). It checks no dependency. The database is shared by every instance, so failing readiness on it would pull them all out of rotation at once and turn a database blip into a full outage; startup already fails without the database, and a later outage shows in the logs, the request errors and `db_tx_duration_seconds` instead (changed 2026-10-23 at the user's request; until then `/readyz` also pinged the database).

The client is static files served by nginx, never by the gateway. In development, Vite serves it and proxies `/api` and `/ws`.

## Concurrency model

Shared state is owned by one goroutine (an actor) that receives commands on a buffered channel. Nothing else touches it, so game state needs no locks.

| Component | Goroutines | Owns |
| --- | --- | --- |
| WebSocket connection | 2 per connection: read loop and write loop | Its send queue (bounded at 256 messages; a full queue closes the connection) |
| Command queue (in the gateway, step 28) | 1 per connection, with a context that ends when the connection closes. The read loop only decodes, applies the rate limits, answers Ping and queues the message; this goroutine runs Hello and then each command in order, calling the service that owns it. The queue holds 64; when it is full, a command with a request ID is answered `rate_limited` and the rest are dropped. | The connection's commands in flight, in arrival order |
| Gateway session registry | A mutex-guarded map, not an actor: its critical sections are two map writes | Which account and character each connection belongs to |
| Map instance | 1 per map and channel, ticking at 20 Hz | Players' positions and combat state, mobs, ground drops |
| World delivery stream (step 27) | One `Deliver` stream per gateway, served from a bounded queue (8,192 events); a player's sink queues on its gateway's stream and never blocks the tick. A full queue ends the stream, the instances drop that gateway's players at their next event, and the gateway subscribes again and rejoins them. | The events bound for every player who joined through that gateway, in order |
| Party service (in the social service, step 25) | A mutex-guarded service, not a goroutine: each command is a few map operations. PartyState and invites go out on the social service's delivery stream to the member's gateway, under that lock, without blocking; each membership change goes to every world node on `WatchParties`. | All parties and invites, plus what it knows of each member (map, level, HP, online) from the gateway and the map instances |
| Party copy (in the world) | A mutex-guarded table fed by `WatchParties` and cleared when that stream breaks; a map tick reads it (members, same party) for microseconds and never calls the social service. Member updates go to a bounded queue a worker sends from. | Each character's party members, as the social service last said |
| Persistence worker | 1 | Batched character checkpoints |
| Economy workers | Pool of 8 | Database transactions for pickups and inventory actions |
| Market (Stage 2) | Runs on the caller's goroutine, request and response | Nothing in memory |
| Expiry job (Stage 2) | 1, every minute | Marking expired listings |
| Cleanup job | 1, every hour | Old idempotency keys and sessions |

## Map instance tick

Every 50 ms:

1. Drain the inbox (joins, leaves, inputs, actions, results from economy workers), up to a per-tick limit.
2. Take each player's next queued input, one per tick in sequence order (with none, the player waits up to 5 ticks, then the last keys are held; a backlog past 4 is cut to the newest 2), and simulate movement in 3 fixed sub-steps of 1/60 s with it. The client predicts with the same inputs in the same ticks (page 7, Frame loop; backend step 18).
3. Run mob behavior and spawns.
4. Resolve the attacks queued this tick.
5. Apply deaths, respawns, EXP, and new drops.
6. Expire old ground drops and loot windows.
7. Send each player a delta with the latest input sequence it has applied for them. In Stage 1 a delta goes to every player every tick, even when empty, so `ack_seq` stays fresh; Stage 2 may switch to interest-managed deltas.
8. Record the tick duration.

**Rule:** a tick never waits on the database. Anything that needs a transaction goes to an economy worker, and the result comes back as an inbox message.

## Pickup: an economic action from inside a tick

1. The player sends PickUp. The tick finds the nearest eligible drop in range, marks it reserved for that player, and hands a job to an economy worker: character, drop contents, and the drop ID as the idempotency key.
2. The worker calls the item service's `Add` (gRPC, page 10), which runs one transaction: add the item (new item or existing stack) with an item event, or, from Stage 2, move coins from `system:mint` to the wallet; write the outbox event; commit.
3. The result goes back to the map instance. On success the drop is removed and the player gets InventoryChanged or WalletChanged. On failure, such as a full inventory, the reservation is released and the drop stays.

Only one player can hold a reservation, so two players can never both get a drop. Using the drop ID as the idempotency key makes a retried job harmless: a worker retries once on a transient error, one that carries no reason across the call (never on a rule error such as a full inventory, which does). The pool's queue holds 1,024 jobs and submitting never blocks the tick; when the queue is full the reservation is released at once and the player gets a Response asking them to try again. InventoryMove and InventoryDiscard do not pass through a tick at all: the gateway calls the item service for them on the connection's own goroutine, with a 5 s timeout, and answers with a Response plus InventoryChanged.

## Character state and checkpoints

- While a character is online, its position, HP, MP, and EXP live in the map instance.
- The persistence worker saves them every 30 seconds (TD-02), and right away on level-up, map change, and logout, through the character service (step 26). A save that fails transiently is retried with backoff, in order, for up to a minute, so a character service restart loses no level-up; on shutdown the queue is flushed with one try each.
- Items and coins never wait for a checkpoint: each change is its own transaction.

## Joining, leaving, and changing maps

- **Join:** the connection's command goroutine runs Hello: the gateway validates it and asks the world service to join the character (step 27). The world loads it from the character service (which checks it is the account's), grants its starter items if creation never did, and joins it to its saved map's instance, which sends the Snapshot on the gateway's delivery stream. The gateway sends Welcome, with the inventory and the party, and then the Snapshot: deliveries that arrive before Welcome wait in the session, and anything that arrives before the join's Snapshot belongs to an older presence of the character and is dropped. A character that is not the account's is a bad Hello (Kicked `unauthenticated`); a world that cannot answer is waited for up to the join timeout (5 s), then the connection closes as a server fault and the client reconnects.
- **Portal:** the instance removes the player, saves a checkpoint, sends Join to the target instance with the player's in-memory state, and sends the client MapChange.
- **Disconnect:** the gateway sends Leave with its connection ID; the world removes the player only if it joined through that connection, and saves it immediately. Reconnecting joins again at the saved position.
- **World restart:** the gateway's delivery stream breaks and every socket stays open. When the stream is back, the gateway joins each player again from their last checkpoint: MapChange first when that is another map, then the Snapshot. Ticks on a connection never go back: the gateway shifts a restarted world's ticks past the last one it sent.
- **Login elsewhere:** a new login revokes the old session in the database; the account service reports it on its `WatchSessions` stream to every gateway, and the gateway holding the old connection closes it with Kicked (step 24; the minute check catches what a broken stream missed).
- **Chat:** the social service cleans and rate-limits a line; a map line comes back to the gateway, which sends it to the world (`Say`) for the sender's map, whose instance sends it to everyone there with the next tick's events; a party line is delivered by the social service to each member's gateway, on any map (step 25).

## Shutdown and crashes

- **Graceful (SIGTERM):** each process turns unready first. The gateway then refuses new connections, tells every client the server is restarting (Kicked `server_restart`) and closes its connections; the world flushes every queued checkpoint before it exits. A service other than the gateway restarts without closing a socket (page 10, Restarts and failures).
- **Crash:** in-memory state since the last checkpoint is lost (positions, mobs, ground drops, parties). Committed items and coins are unaffected. A world crash keeps the sockets: the gateway rejoins its players when the world is back.

## Metrics

- `tick_duration_seconds` histogram per map; `tick_overruns_total` (on the world's ops `/metrics`)
- `players_online`
- `ws_messages_total` by direction and type; `ws_slow_consumer_disconnects_total`
- `db_tx_duration_seconds` by operation; `db_tx_retries_total`
- `economy_jobs_inflight`
- `outbox_unpublished`: grows in Stage 1, then shows relay lag in Stage 2
