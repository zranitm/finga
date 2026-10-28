# 09. World: map instances and the tick

- **Depends on:** 03, 04, 06
- **Features:** B1, B2
- **Milestone:** M1. Walk together
- **Order:** 9
- **Status:** Done
- **Step:** 09. World: map instances and the tick

> **Changed by step 26 (2026-10-28):** checkpoints and level-ups go to the character service, with the world's own `Checkpoint` and `LevelUpSave` types; the persistence worker retries a transient failure, in order, for up to a minute.

**Goal:** maps that tick at 20 Hz, accept players, simulate their movement, and send each player what changed.

**Layer in this step:** application only. The world has no repository: it reaches the database only through other modules' services, from worker goroutines.

## Scope

- `pkg/tick`: a fixed-timestep runner with an overrun metric, testable with `testing/synctest`.
- `internal/application/world`:
  - `Instance`, one actor per map: an inbox channel, a per-tick drain limit, join and leave.
  - Inputs queued by sequence number, then 3 movement sub-steps per tick through `sim` (TD-18: the server simulates from inputs).
  - The world's own result types: a full view on join, a change set every tick with `ack_seq`, and a teleport notice for respawns and portals. The gateway turns them into Snapshot, Delta, and Correction messages in step 10, so this package never imports the protocol.
  - A registry with one instance per map, on one channel.
  - The persistence worker: checkpoints batched every 30 seconds (TD-02), and on leave, saved through the character service.

## Design notes

- An instance sends to players through a small interface it declares, which never blocks: the gateway enqueues on the connection, and a full queue drops that player, never the tick.
- Tick timing and overruns are reported through an observer interface the package declares; `internal/telemetry` implements it.

## Tests first

Time runs under `synctest`, so these are exact and instant:

- a player joining gets a Snapshot, then Deltas; others see the join and the leave
- inputs apply in sequence order; old and duplicate sequence numbers are ignored; `ack_seq` is reported
- an input flood beyond the drain limit carries over to the next tick
- a slow tick increments the overrun metric
- a checkpoint is saved every 30 seconds and on leave (a fake store records the calls)
- replaying recorded inputs gives the same states as running `sim` directly

## Done when

- [ ] Tests pass under `-race`.

## Review focus

- Nothing inside a tick blocks: no database call, no blocking channel send.
- Instances share no state with each other.
