# 28. Gateway service

- **Depends on:** 24, 27
- **Features:** A1, B2
- **Milestone:** Services
- **Order:** 28
- **Status:** Done
- **Step:** 28. Gateway service

**Goal:** what is left of the game server becomes the gateway: the only public entry, with no game rules and no database, whose read loop never blocks. CI builds and tests each service on its own, and the contracts are checked for breaking changes.

**Layers in this step:** handler (the gateway) and packaging.

## Scope

**Gateway**

- `cmd/gameserver` becomes `services/gateway/cmd/gateway`, `internal/handler` becomes `services/gateway/internal/handler`, and the adapters left in the composition root move to `services/gateway/internal/clients`.
- Per-player command queue: bounded (64), drained in order by one goroutine per connection, with a context that ends when the connection closes. The read loop decodes, rate-limits and routes only; Hello runs on the queue; a full queue answers `rate_limited` to a command with a request ID and drops the rest. Ping is answered on the read loop.
- Drain on SIGTERM: readiness turns 503, every connection gets Kicked `server_restart` and closes, then the listener shuts down within the grace period.
- depguard: the gateway imports no database package.

**Packaging and CI**

- Compose: Postgres, migrate, and the six services, built from the one Dockerfile with `SERVICE`; BuildKit cache mounts for Go's caches.
- Make: `test-service SERVICE=<name>`; `run` starts migrate and the six binaries without Docker.
- CI: a matrix job builds and tests each service on its own; the go job runs everything together; `buf breaking` against the previous commit.

**Docs**

- README (layout, build and run, architecture), page 1 (tree, rules), page 3 (processes, concurrency, joining), page 4 (where the protocol is served), the High-level design and the Stage 1 page describe the split as built.

## Tests first

- a world call that blocks does not stop the read loop: a Ping on the same connection is answered while the command waits
- commands reach the services in the order they arrived
- a full command queue answers `rate_limited` to a command with a request ID
- closing the connection cancels the context of its command in flight
- drain: readiness turns 503, then every connection receives Kicked `server_restart` before it closes
- `make test-service SERVICE=<name>` passes for each service

## Done when

- [x] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; CI green, including the matrix and `buf breaking`.

## Review focus

- No game rule in the gateway: it decodes, limits, routes and translates.
- The drain order: no new connection is accepted after readiness turns off.
