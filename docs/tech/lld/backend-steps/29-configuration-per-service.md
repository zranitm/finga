# 29. Configuration per service

- **Depends on:** 28
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 29
- **Status:** Ready
- **Step:** 29. Configuration per service

**Goal:** each binary reads its own configuration file, with only its keys, and its own configuration type; the shared file and type are gone (page 11, Configuration per service).

**Layers in this step:** configuration and the composition roots.

## Scope

- `services/<name>/configs/config.yaml` and `services/<name>/internal/config` for the six services, and `cmd/migrate/configs/config.yaml` with its type in `cmd/migrate`.
- `internal/host`: the sections every service has (`grpc`, `ops`, `shutdown_grace`, `log_level`) as types the services' `Config` embed; `host.Main` and `host.Start` load the service's own file, from `services/<name>/configs` relative to where the binary runs.
- Each composition root takes its own `Config`; the end-to-end and process tests build each service's.
- `server/configs/` and `internal/config` removed.
- Dockerfile: the image holds the binary's own file at the same path under `/app`. Compose sets each service's own environment (`FINGA_DEPENDENCIES_*`, `FINGA_DATABASE_URL`, `FINGA_HTTP_ADDR`). `make run` starts each binary with its file. `server/.env.example`, the README's Configuration section and page 1 follow.

## Tests first

- each service's defaults load and pass their checks, with the values the old shared file had for that service
- a key another service has (`database` in the social service's file) is refused by the strict loader
- an override reaches only the service that has the key: `FINGA_DATABASE_URL` changes the item service's database, `FINGA_DEPENDENCIES_ITEM` the world's item address
- `host.Start` reads the service's own file and fails with the file's path in the message when it is missing

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; CI green.

## Review focus

- No binary reads another's keys; nothing is left in a shared file.
