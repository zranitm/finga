# 29. Configuration per service

- **Depends on:** 28
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 29
- **Status:** Done
- **Step:** 29. Configuration per service

**Goal:** each binary reads its own configuration file, with only its keys, and its own configuration type; the shared file and type are gone (page 11, Configuration per service).

**Layers in this step:** configuration and the composition roots.

## Scope

- `services/<name>/config/`: `config.yaml` and the `Config` type beside it, for the six services, and `cmd/migrate/config/` the same way. Outside `internal/`, so the end-to-end tests build each service with its own (decision 259).
- `internal/host`: the types of the sections every service has (`GRPCSection`, `OpsSection`) and `Settings`, which each `Config` hands it through `Host()`; `host.Main` and `host.Start` are generic over the service's `Config` and load its own file, `services/<name>/config` relative to where the binary runs. `pkg/dbx` gets the database section's type, `dbx.Config`.
- Each composition root takes its own `Config`; the end-to-end and process tests build each service's.
- `server/configs/` and `internal/config` removed.
- Dockerfile: the image holds the binary's own file at the same path under `/app`. Compose sets each service's own environment (`FINGA_DEPENDENCIES_*`, `FINGA_DATABASE_URL`, `FINGA_HTTP_ADDR`). `make run` starts each binary with its file. `server/.env.example`, the README's Configuration section and page 1 follow.

## Tests first

- each service's defaults load and pass their checks, with the values the old shared file had for that service
- a key another service has (`database` in the social service's file) is refused by the strict loader
- an override reaches only the service that has the key: `FINGA_DATABASE_URL` changes the item service's database, `FINGA_DEPENDENCIES_ITEM` the world's item address
- `host.Start` reads the service's own file and fails with the file's path in the message when it is missing

## Done when

- [x] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green; CI green.

## Review focus

- No binary reads another's keys; nothing is left in a shared file.
