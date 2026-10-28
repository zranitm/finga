# 08. WebSocket transport and message catalogue

- **Depends on:** 01
- **Milestone:** M1. Walk together
- **Order:** 8
- **Status:** Done
- **Step:** 08. WebSocket transport and message catalogue

> **Changed by step 28 (2026-10-28):** the gateway answers Ping on the read loop and queues every other message on the connection's command queue (64 deep), drained in order by one goroutine per connection; a full queue answers `rate_limited` to a command with a request ID.

**Goal:** a generic, well-behaved WebSocket layer, and the Go side of the message catalogue.

## Scope

- `pkg/wsconn`, on `coder/websocket`:
  - Upgrade with an origin check against the allowed origins.
  - One read loop and one write loop per connection, pings with timeouts, and a read size limit.
  - A bounded send queue of 256 messages; when it's full, the connection is closed.
  - The `Codec` interface and a JSON codec with a type registry, using page 4's envelope (`t`, `req`, `seq`, `tick`, `d`).
  - The `Handler` interface (`OnOpen`, `OnMessage`, `OnClose`) and close codes.
- `internal/protocol`: the Stage 1 message structs from page 4, registered with the codec.
- `api/asyncapi.yaml`, the WebSocket message catalogue.
- `testdata/contracts/`: one golden JSON file per message type. The client's tests read the same files.

## Design notes

- Only the write loop writes to the socket. Everything else enqueues, so writes never race.
- `internal/protocol` holds the Go message types; page 1's tree lists it.

## Tests first

- every golden file decodes into its type and re-encodes to the same JSON
- an unknown message type, and a frame over the size limit, are rejected
- a full send queue closes the connection (a slow reader)
- a missed pong closes the connection
- a disallowed origin is refused at the upgrade with 403
- `OnClose` runs exactly once, and no goroutines leak (`goleak`)

## Done when

- [ ] Tests pass, including under `-race`.

## Review focus

- Close handling, and goroutine lifetimes on every exit path.
