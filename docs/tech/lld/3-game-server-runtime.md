# 3. Game server runtime

## Process

One binary per job, as in docviewer:

- `cmd/gameserver` runs the game.
- `cmd/migrate` applies the migrations and exits.
- `cmd/bot` drives test players against a running server for the proof session and prints the tick percentiles from `/metrics`.
- `cmd/auditor` (Stage 2) checks the invariants continuously, as a Kafka consumer.

The game server starts one HTTP server, the world's map instances, and the background workers. Routes: REST under `/api`, WebSocket at `/ws`, `/healthz` for liveness, `/readyz` for readiness, and metrics at `/metrics`.

**Readiness** means this process can take traffic: `/readyz` is 200 from the moment it listens (game data loaded, everything wired) until shutdown starts, and 503 before and after, so a load balancer stops sending new connections while in-flight requests drain. It checks no dependency. The database is shared by every instance, so failing readiness on it would pull them all out of rotation at once and turn a database blip into a full outage; startup already fails without the database, and a later outage shows in the logs, the request errors and `db_tx_duration_seconds` instead (changed 2026-10-23 at the user's request; until then `/readyz` also pinged the database).

The client is static files served by nginx, never by the game server. In development, Vite serves it and proxies `/api` and `/ws`.

## Concurrency model

Shared state is owned by one goroutine (an actor) that receives commands on a buffered channel. Nothing else touches it, so game state needs no locks.

| Component | Goroutines | Owns |
| --- | --- | --- |
| WebSocket connection | 2 per connection: read loop and write loop | Its send queue (bounded at 256 messages; a full queue closes the connection) |
| Gateway session registry | A mutex-guarded map, not an actor: its critical sections are two map writes | Which account and character each connection belongs to |
| Map instance | 1 per map and channel, ticking at 20 Hz | Players' positions and combat state, mobs, ground drops |
| Party service | A mutex-guarded service, not a goroutine: each command is a few map operations, and a map tick asks it (members, same party) under the same lock for microseconds. PartyState and invites go out through the gateway's non-blocking sends, under that lock. | All parties and invites, plus what it knows of each member (map, level, HP, online) from the gateway and the map instances |
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
2. The worker runs one transaction: add the item (new item or existing stack) with an item event, or, from Stage 2, move coins from `system:mint` to the wallet; write the outbox event; commit.
3. The result goes back to the map instance. On success the drop is removed and the player gets InventoryChanged or WalletChanged. On failure, such as a full inventory, the reservation is released and the drop stays.

Only one player can hold a reservation, so two players can never both get a drop. Using the drop ID as the idempotency key makes a retried job harmless: a worker retries once on a transient error (never on a rule error such as a full inventory). The pool's queue holds 1,024 jobs and submitting never blocks the tick; when the queue is full the reservation is released at once and the player gets a Response asking them to try again. InventoryMove and InventoryDiscard do not pass through a tick at all: the gateway runs them on the connection's own goroutine, with a 5 s timeout, and answers with a Response plus InventoryChanged.

## Character state and checkpoints

- While a character is online, its position, HP, MP, and EXP live in the map instance.
- The persistence worker saves them every 30 seconds (TD-02), and right away on level-up, map change, and logout.
- Items and coins never wait for a checkpoint: each change is its own transaction.

## Joining, leaving, and changing maps

- **Join:** the gateway validates Hello, loads the character, and sends Join to the map instance for the character's saved map. The instance replies with Welcome and a Snapshot.
- **Portal:** the instance removes the player, saves a checkpoint, sends Join to the target instance with the player's in-memory state, and sends the client MapChange.
- **Disconnect:** the player is removed from the map and saved immediately. Reconnecting joins again at the saved position.
- **Login elsewhere:** a new login revokes the old session in the database, and the gateway closes the old connection with Kicked.
- **Chat:** the chat service cleans and rate-limits a line; a map line is handed to the sender's map instance, which sends it to everyone on the map with the next tick's events; a party line is delivered by the gateway to each member's connection, on any map.

## Shutdown and crashes

- **Graceful (SIGTERM):** stop accepting connections, tell clients the server is restarting, flush all checkpoints, close.
- **Crash:** in-memory state since the last checkpoint is lost (positions, mobs, ground drops, parties). Committed items and coins are unaffected.

## Metrics

- `tick_duration_seconds` histogram per map; `tick_overruns_total`
- `players_online`
- `ws_messages_total` by direction and type; `ws_slow_consumer_disconnects_total`
- `db_tx_duration_seconds` by operation; `db_tx_retries_total`
- `economy_jobs_inflight`
- `outbox_unpublished`: grows in Stage 1, then shows relay lag in Stage 2
