# 10. Gateway

- **Depends on:** 05, 08, 09
- **Features:** B2
- **Milestone:** M1. Walk together
- **Order:** 10
- **Status:** Done
- **Step:** 10. Gateway

> **Changed by step 27 (2026-10-28):** Hello joins through the world service, and the world's events reach the gateway on one delivery stream per gateway, translated to protocol messages there. Deliveries that arrive before Welcome wait in the session; when the stream breaks, every socket stays open and each player is joined again.

> **Changed by step 28 (2026-10-28):** the gateway is a service of its own, `services/gateway`. Hello and each command run on the connection's command goroutine, so a slow service never stalls the read loop; on SIGTERM every connection gets Kicked `server_restart` before the listener closes.

**Goal:** players connect, authenticate once, and move together on a map. This completes M1, Walk together.

**Layer in this step:** handler. The gateway calls the account service, the world, and later other services. It never touches a repository.

## Scope

- `internal/handler/ws` (the gateway):
  - Hello within 5 seconds, carrying the token, protocol version, and a character ID that must belong to the account. Welcome in reply.
  - The session registry: connection to `Session`, and account to connection.
  - One active connection per account: a new login kicks the old one with Kicked.
  - Routing each message to the character's map instance, stamped with the identity from the connection.
  - Per-account rate limits from page 4.
  - Kicked reasons, and the protocol-version check.
- `/ws` mounted in `internal/server`.
- Translating the world's result types (step 09) into Snapshot, Delta, and Correction messages, so only the gateway knows the protocol.

## Design notes

- Identity comes from the connection, never from a message. The registry is the only place that maps a connection to an actor.

## Tests first

Handler tests use fakes for the account service and the world; the end-to-end rows use the real ones.

- no Hello within 5 seconds closes the connection
- a bad token, someone else's character, or an old protocol version closes it with the right reason
- any message before Hello is rejected
- a message naming another character's ID still acts as the sender
- a second login for the same account kicks the first connection
- rate limits: Input over 20 per second is dropped
- end-to-end with two Go test clients: both join the town, and each sees the other move
- B2: sending inputs faster than allowed doesn't make a character move faster

## Done when

- [ ] M1 is demonstrable with two test clients (and with the browser client once it exists).

## Review focus

- Registry cleanup when connections close, so no session leaks.
- What happens on every Hello failure path.
