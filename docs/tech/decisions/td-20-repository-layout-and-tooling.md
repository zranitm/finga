# TD-20. Repository layout and tooling

- **Area:** Architecture
- **Decision:** TD-20. Repository layout and tooling
- **Default:** Follow docviewer
- **Outcome:** Follow zranitm/docviewer's layout and tooling: api/, cmd/ with one binary per job, configs/ YAML with env overrides, deploy/ with one Dockerfile, docs/ exported from Notion, internal/, pkg/, test/, testdata/, tools/, web/. Same Makefile targets, golangci-lint setup, and test conventions.
- **Status:** Decided

## Reused unchanged from docviewer

- `pkg/configload`: typed YAML configuration with environment overrides, validated at start.
- `.golangci.yml`, `.gitattributes`, and the Makefile's `up`, `down`, `test`, `cover`, and `lint` targets.
- `tools/docs-export`: the Notion-to-HTML converter, pointed at this project's pages. Never brought over: on 2026-10-23 the design moved from Notion into `docs/` as Markdown, so there is nothing to export.

## Added for this project

- `data/`: game content and Tiled maps.
- `migrations/`: goose migrations, embedded through `embed.go`.
- `internal/application`: every business module in one place, apart from transport and infrastructure. It never imports handlers, protocol, telemetry, or config.
- `internal/server`: wiring and the HTTP server, since there are two handlers (REST and WebSocket). Named `server` rather than `app`, so it can't be confused with `application`. **Superseded on 2026-10-23** at the user's request: each binary wires its own dependencies next to its main (the Composition Root), so the game server's wiring and process moved to `cmd/gameserver/app`, and the route table and probes to `internal/handler/router.go` (Low-level design, page 1).
- A `gen` Make target for sqlc.
- `services/`: **added on 2026-10-27 by TD-25** and built in backend steps 20 to 28: one folder per service (gateway, account, character, item, world, social), each with its `cmd/`, `internal/` and `migrations/`. `internal/application`, `internal/handler`, `cmd/gameserver` and the top-level `migrations/` moved into the services; `cmd/` keeps `migrate` and `bot` (Low-level design, pages 1 and 10).

## Consequences

- `migrate` and `auditor` are their own binaries instead of subcommands.
- The client moves from `client/` to `web/`, and Protobuf from `proto/` to `api/proto/`.
- Decision records stay in Tech decisions and are exported to `docs/`, instead of a separate `docs/adr`.

## Details

Low-level design, page 1: Codebase and modules.
