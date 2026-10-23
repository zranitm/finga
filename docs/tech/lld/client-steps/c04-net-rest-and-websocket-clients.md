# C04. net: REST and WebSocket clients

- **Depends on:** C01, C02
- **Features:** A1
- **Milestone:** Foundations
- **Order:** 4
- **Status:** Done
- **Step:** C04. net: REST and WebSocket clients

**Goal:** the only two ways the client talks to the server, each testable with fakes.

**Layer in this step:** `net`, and the `testing/` helpers it needs (page 7 layer 3).

## Scope

- `net/token.ts`: the one holder of the session token, read by both `rest.ts` and `socket.ts`. Set from the login response, replaced only when a Welcome arrives, cleared on logout or a Kicked that ends the session. Memory only (page 4, Trust model). Nothing else keeps a copy: a REST call with the token from before the last Hello works once within 60 s and then revokes the session.
- `net/rest.ts`: typed calls for the Stage 1 REST API from page 4: `POST /api/accounts`, `POST /api/sessions`, `DELETE /api/sessions/current`, `GET` and `POST /api/characters`, `GET` and `PUT /api/settings`; errors mapped from the server's error body to a `RestError` with its code; the session token sent as the server expects.
- `net/socket.ts`: a `GameSocket` over the browser `WebSocket` and a `Codec`:
  - `connect(characterId)` opens `/ws`, sends Hello (the token from `token.ts`, `protocol_version: 1`, the character ID) within the server's 5 s window, and resolves on Welcome, whose token replaces the held one;
  - `send(msg)` for fire-and-forget messages; `request(msg)` returns a promise matched by `req` to its `Response`, with a timeout;
  - a Response with no `req` (a failed pickup, a refused chat line) goes to an `onUnmatchedResponse` handler instead of being dropped;
  - a request still pending when the socket reconnects is re-sent once on the new connection, with a new `req` and the same payload, so its idempotency key survives the reconnect;
  - a sequence counter for Input, Attack, PickUp and EnterPortal;
  - Ping every 15 s with a Pong deadline, to spot a dead connection and estimate round-trip time and server time. The server's own protocol-level pings (every 20 s, 10 s timeout) are what close a silent connection with 4002;
  - the server's close codes (4001 slow consumer, 4002 no pong, 4003 kicked, 4004 bad message) and the Kicked reason before a 4003 surfaced as typed events;
  - reconnect with `lib/backoff` and a fresh Hello after 4001, 4002, a dropped connection, or Kicked `server_restart`. No reconnect after any other Kicked reason (`unauthenticated` and `session_expired` clear the token and return to login; `logged_in_elsewhere` and `abuse` show the reason; `outdated_protocol` reloads the page), after 4004 (a client bug that a retry would repeat), or after the user closes it;
  - typed `on(messageType, handler)` subscriptions.
- `testing/fake-socket.ts`: a WebSocket double that records sent frames and lets a test push server frames, plus `testing/reset-stores.ts` for later steps.

## Design notes

- `net` knows nothing about stores or Phaser; `app/wiring` (C06) connects them.
- One Input per 50 ms tick while in the world (page 7, Sending): the caller hands over each tick's sampled keys, and the socket refuses a second Input in the same tick, so no caller can exceed the server's limit (30 a second, backend step 18).

## Tests first

- REST: each endpoint builds the right method, path and body; a 409 on `POST /api/characters` maps to the conflict code; the token header is present after login
- Hello is the first frame, with `protocol_version: 1`; Welcome resolves `connect` and replaces the token, and the next REST call sends the new one
- `request` matches `Response` by `req`, rejects on the server's error code, and times out
- Input: a second Input in the same tick is refused; one Input per tick goes out in sequence order
- a Response with no `req` reaches `onUnmatchedResponse`
- a request pending across a reconnect is re-sent once with the same idempotency key
- close 4002 reconnects with backoff and a new Hello; Kicked `server_restart` then 4003 reconnects; Kicked `unauthenticated` then 4003 clears the token and does not reconnect; close 4004 does not reconnect
- an unknown frame from the server is reported, not thrown

## Done when

- [ ] Tests pass with 80% coverage on `net`.
- [ ] A smoke script in `web/scripts/` signs up, logs in and receives Welcome against `make up` (run by hand; recorded in the log row).

## Review focus

- No message leaves without a sequence number or request ID where the golden files have one.
- Reconnect cannot run twice at once.
- Only `token.ts` stores the token.
