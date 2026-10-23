# 18. Client follow-up: one Input per tick

- **Depends on:** 09, 10
- **Features:** B2
- **Milestone:** M1. Walk together
- **Order:** 18
- **Status:** Done
- **Step:** 18. Client follow-up: one Input per tick

**Goal:** the server applies exactly the inputs the client predicted with, in the same ticks, so client prediction (client step C08) matches the server state exactly instead of snapping.

**Layers in this step:** application (world) and handler (gateway rate limits). Found by the client steps review: today a tick keeps only the last Input received and applies it to all 3 sub-steps, so an Input delayed into the next tick replaces that tick's own, and a key tap shorter than a tick can vanish.

## Scope

**Application** (`internal/application/world`)

- Each player keeps a small queue of received inputs, ordered by sequence number. Every tick takes exactly one, the oldest not yet applied, and runs the 3 sub-steps with it.
- No input this tick: keep the last held keys (a late packet, or a client that has stopped sending).
- More than 4 inputs queued (a burst after a stall): drop the oldest down to 2, so the player catches up within a few ticks instead of lagging behind for good.
- An input with a sequence number already applied, or older, is ignored, as today.
- `ack_seq` in the Delta is the sequence number of the input applied this tick.
- While dead, the queue is emptied every tick, as today's `kill` does.

**Handler** (`internal/handler/ws`)

- Input rate limit from 20 to 30 per second, burst 30: the client now sends one per tick (20 a second), and the headroom absorbs a burst after a stall.

## Design notes

- The client sends one Input every tick while in the world, with the keys it sampled for that tick (page 4, page 7). Without a queue, two inputs arriving in one tick collapse into one and the server skips a tick of input; with it, both sides apply the same input sequence.
- Pages 3 and 4 already describe this behaviour (one per tick, 30 per second); this step makes the code match.

## Tests first

- one input per tick is applied in sequence order
- three inputs arriving in one tick are applied over the next three ticks, one each
- a tick with no input keeps the previous keys held
- six queued inputs are cut to the newest two
- `ack_seq` equals the sequence number applied that tick
- exact match: a player's state after N ticks equals running `sim.Step` 3 times per tick over the same inputs, compared with `==` like the movement vectors
- the gateway accepts 30 Inputs in a second and drops the 31st

## Done when

- [ ] Tests pass, and the bot session from step 17 still runs clean.

## Review focus

- The queue is bounded, so a client that floods inputs can't grow server memory.
- The tick still never waits: the queue is filled from the inbox drain only.
