# 07. REST API and server wiring

- **Depends on:** 05, 06
- **Features:** A1, A2, B6
- **Milestone:** M1. Walk together
- **Order:** 7
- **Status:** Done
- **Step:** 07. REST API and server wiring

> **Changed by step 24 (2026-10-27):** the REST handler calls the account service through its client and checks tokens through `auth.Guard`, an adapter over it.

> **Changed by step 26 (2026-10-28):** the REST handler lists and creates characters through the character service's client; the game server has no database pool any more.

**Goal:** a running `cmd/gameserver` with the REST endpoints from page 4.

**Layers in this step:** the REST handler layer, on top of the account and character services from steps 05 and 06, plus the server that wires them.

## Scope

**Handler** (`internal/handler/api`)

- Endpoints: `POST /api/accounts`, `POST /api/sessions`, `DELETE /api/sessions/current`, `GET` and `POST /api/characters`, `GET` and `PUT /api/settings`.
- DTOs and decoding: rejects malformed JSON, missing fields, and bodies over the size limit. Business rules stay in the services.
- Middleware: authentication (Bearer token to `Principal`, through `pkg/auth`'s `Sessions` interface), request logging, CORS from `server.cors_origins`, and panic recovery.
- Error mapping: `errs` codes to HTTP status, with a body of `code` and `message`.
- Each endpoint calls exactly one service method, through an interface the handler package declares.

**Server** (`internal/server` and `cmd/gameserver`)

- Wiring: builds each module's store over the pool, then the services and handlers, and fills the services' option structs from configuration.
- The HTTP server with `/healthz`, `/readyz` (game data loaded and the database answering), and `/metrics`, plus graceful shutdown.
- `cmd/gameserver/main.go`, kept thin as in docviewer: load config, build, run until SIGINT or SIGTERM.
- `api/openapi.yaml`.

## Design notes

- Routing uses the standard library's `http.ServeMux`, which matches methods and path patterns since Go 1.22, so there's no web framework to learn or carry. Open for your review.

## Tests first

**Handler** (fake services, `httptest`)

- every endpoint: the happy path, malformed input, and each service error code mapped to its status
- authentication: missing, malformed, and revoked tokens all give 401 `unauthenticated`
- CORS with no allowed origins, and with one
- a panic in a service gives 500 `internal`, with no internal text in the body
- a drift check comparing the routes and DTOs with `api/openapi.yaml`, as in docviewer

**Server**

- `/readyz` before and after startup; shutdown lets in-flight requests finish

**End-to-end** (`test/`, the server built as production builds it, real Postgres)

- A1: sign up, log in, throttled after repeated failures
- A2: create a character
- B6: settings saved and returned

## Done when

- [ ] Handler tests and the end-to-end rows pass.

## Review focus

- No business rule in a handler: a handler that checks a username's length is in the wrong layer.
- Internal error text never reaches a response. Every 5xx is logged with its stack.
- Tokens never appear in logs.
