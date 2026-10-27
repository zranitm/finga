# 24. Account service

- **Depends on:** 22
- **Features:** A1
- **Milestone:** Services
- **Order:** 24
- **Status:** Done
- **Step:** 24. Account service

**Goal:** accounts and sessions run as their own service. The REST handler and the gateway authenticate over gRPC, and the kicks the account service used to call directly arrive through a stream.

**Layers in this step:** the account module (moved), its gRPC handler and binary; the game server's adapters.

## Scope

- `internal/application/account` moves to `services/account/internal/account` as it is.
- `services/account/internal/handler`: `AccountService` with its error table, and `WatchSessions` over `pkg/hub`: the module's two hooks (session revoked, account revoked) publish to every subscribed gateway.
- `services/account/cmd/account` and `app`.
- Game server: an adapter implements `pkg/auth`'s sessions over the account client for the REST middleware and the gateway; the REST handler calls register, login, log out, log out everywhere and settings through it; a task follows `WatchSessions` and calls the gateway's `KickSession` and `KickAccount`. The minute check stays, and catches what a broken stream missed.
- `errmap` maps the account reasons to the same codes.
- Compose and the end-to-end stack run the account service.

## Tests first

- handler over `bufconn`: each RPC maps its fields (table); each account error arrives with its reason (table); the guard over the module's errors
- `WatchSessions`: a subscriber first gets Subscribed with the boot ID; a revoked session and a new login reach every subscriber; a second subscription with the same gateway ID replaces the first
- game server: a revoked session from the stream kicks that connection with `session_expired`; a new login kicks the older connection with `logged_in_elsewhere`; after the stream breaks the game server subscribes again
- end-to-end rows unchanged: A1, the second login kicking the first, log out everywhere closing the game connection

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green.

## Review focus

- The login throttle still sees the client's IP, now a field of `Login`.
- Nothing about a token is logged on either side.
