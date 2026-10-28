# 27. World service

- **Depends on:** 23, 25, 26
- **Features:** B1, B2, B3, C1 to C4, D1
- **Milestone:** Services
- **Order:** 27
- **Status:** Done
- **Step:** 27. World service

**Goal:** the map instances run as their own service. The gateway joins players through the world, receives their events on a delivery stream, and when the world restarts it keeps every socket open and rejoins its players.

**Layers in this step:** the world and combat modules (moved), the world's handler, its clients and binary; the game server's Hello, delivery translation and rejoin.

## Scope

**World service**

- `internal/application/world` and `combat` move to `services/world/internal/world` and `combat` as they are.
- Join by ID: `World.Join(ctx, account, character, sink)` loads the character from the character service (ownership checked there), grants the starter items through the item service (the safety net; a failure fails the join, and the client's retry tries again), joins the character's map instance, and returns the character as the player sees it.
- A presence remembers its connection ID; a `Leave` naming another connection removes nothing.
- `services/world/internal/handler`: `WorldService`. `Deliver` keeps one bounded queue per gateway (`pkg/hub`); a player's sink queues on its gateway's stream and never blocks; a full queue closes that stream. Commands carry the map ID and go to that instance.
- `services/world/internal/clients`: the character client (load, checkpoints, level-ups), the item client (pickups, starter items) and the party copy, moved from the game server.
- `services/world/cmd/world` and `app`: the instances, the economy workers, the persistence worker, the party copy's tasks; tick metrics on its ops listener.

**Game server**

- Hello: validate and rotate, `world.Join`, the inventory and the party, then Welcome; deliveries that arrive before Welcome wait in the session and follow it.
- The gateway follows `world.Deliver` and turns each event into the protocol message `sink.go` built before. A Teleport moves the session to the target map.
- Commands become world calls with the session's character and map.
- Rejoin: when the stream breaks, every socket stays open; once the stream is back, each player is joined again from their checkpoint, with MapChange first if their map changed, then the Snapshot.

**Tools**

- `cmd/bot` reads the tick percentiles from the world's ops address (`-metrics`); `make bots` passes it. Compose publishes the world's ops port.

## Tests first

- world: Join loads and grants before joining, and returns the view; a grant failure fails the join; someone else's character fails as not found; a `Leave` with another connection ID keeps the player
- handler over `bufconn`: Join's Snapshot arrives on the gateway's stream, then Deltas each tick; a full queue ends that stream; an Input for a map the player is not on changes nothing
- game server: a delivery that arrives before Welcome follows it; after the stream breaks, every player is joined again and gets the Snapshot, with MapChange first when the saved map differs; no socket closes
- end-to-end: every existing row; new row: the world restarts while two players are connected, both sockets stay open and both get a Snapshot again; the crash test kills the world and item processes mid-pickup and keeps the invariants

## Done when

- [x] `make up`, `make smoke`, `make bots` (a short run), `make test`, `make lint`, `make cover` green.

## Review focus

- The order on a player's connection: Welcome, then the Snapshot, then Deltas, in every path including the rejoin.
- Nothing in a tick waits on another service.
