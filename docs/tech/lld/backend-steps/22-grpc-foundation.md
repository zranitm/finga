# 22. gRPC foundation

- **Depends on:** 20, 21
- **Milestone:** Services
- **Order:** 22
- **Status:** Ready for review
- **Step:** 22. gRPC foundation

**Goal:** the internal contracts and the generic plumbing every service needs, before any service leaves: the five proto packages, code generation pinned as Go tools, errors that keep their reason across a call, a process runner, and a subscriber hub for the delivery streams.

**Layers in this step:** api (contracts), pkg, config. Nothing calls the contracts yet.

## Scope

**Contracts and tooling**

- `api/proto/internal/{account,character,item,world,social}/v1/*.proto`, as sketched on page 10, each with `enum Reason`. Package `internal.<service>.v1`, Go package `finga/api/gen/<service>/v1`.
- `buf`, `protoc-gen-go` and `protoc-gen-go-grpc` pinned as Go tools in `server/go.mod`; `buf.yaml` (lint: STANDARD) and `buf.gen.yaml` in `server/`.
- `make gen` runs sqlc and `buf generate`; `make lint` adds `buf lint`; CI checks that the committed generated code is current.

**pkg/grpcx**

- `NewServer`: a gRPC server with interceptors that recover a panic as `Internal`, carry the request ID (`x-request-id`) into the context, and turn a returned error into a status through the service's error table.
- `ErrorTable`: rows of (sentinel, gRPC code, reason enum value). A known error leaves with `ErrorInfo` (reason name, proto package as domain); an unknown one leaves as `Internal` with no text.
- `Dial`: a client connection (insecure in Stage 1) that sends the request ID.
- `ReasonOf(err)` and `IsRule(err)` on the calling side; `Key(enum value)` names a reason the way `ReasonOf` reports it, for tables.
- `Follow`: keeps a server stream open, re-subscribing with backoff after a break, until the context ends; reports each break.

**pkg/runner**

- Runs a binary's listeners (HTTP and gRPC) and background tasks, with readiness (`/readyz` 503 before listening and once draining) and graceful shutdown within a grace period; the game server's `app.Server` is rebuilt on it. `Probes` serves `/healthz`, `/readyz` and `/metrics` for a service's ops listener.

**pkg/hub**

- Subscribers keyed by ID, each with a bounded queue: send to one, broadcast to all; a full queue closes that subscription (its stream ends); subscribing again with the same ID replaces the old one.

**Configuration**

- A `services` section: `listen`, `addr` and `ops` for account, character, item, world and social, validated; defaults on page 10.

## Design notes

- Generated code is committed, so the Docker build and CI need no generator.
- The contracts change freely until step 28 adds `buf breaking`: nothing runs them in production before then.

## Tests first

- grpcx: a sentinel in the table arrives as its code with `ReasonOf` naming its reason, also when wrapped with `%w` (table); an unknown error arrives as `Internal`, its text nowhere in the status; `IsRule` is true for a reason and false for `Unavailable` and a plain `Internal`; a panic in a handler is `Internal` and the server keeps serving; the request ID set by the caller is in the handler's context
- grpcx `Follow`: delivers each message; after the server ends the stream it subscribes again and reports the break; it stops when its context ends
- runner: readiness is 503 before `Run`, 200 while serving, 503 once shutdown starts while a request is in flight (the game server's existing process tests, moved); a busy address fails `Run`; tasks stop when it stops; a gRPC server stops gracefully within the grace period
- hub: a message sent to one subscriber reaches only it; a broadcast reaches all; a full queue closes that subscription and later sends report false; subscribing again with the same ID closes the old subscription
- config: the `services` section is required, and an empty address names its key
- `buf lint` passes on the five contracts

## Done when

- [ ] `make gen` regenerates the committed code byte for byte; `make test`, `make lint`, `make cover` green.

## Review focus

- No error text crosses a call: only the code and the reason.
- The contracts against page 10: every field a flow needs, and every request names its routing key.
