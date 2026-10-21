# AI coding log

One row per backend implementation step (Low-level design, page 8). Evidence
for the process: what was produced, how it was verified, what changed,
mistakes caught, and the commit. Moved to Notion by the design session.

Tool for every row: Claude Code, model Claude Fable 5.1 (`claude-fable-5-1`),
running as the "finga-impl" session from the Notion product and tech pages.

| Date | Step | Task | What it produced | How verified | What the user changed | Mistakes caught | Commit |
|------|------|------|------------------|--------------|-----------------------|-----------------|--------|
| 2026-10-21 | 00 | Repository skeleton | Layout per LLD page 1 (top-level folders, `cmd/gameserver`, `cmd/migrate`), `go.mod` (module finga, go 1.27), Makefile (up/down/test/cover/lint/gen), `.golangci.yml` with depguard rules for the import direction, `pkg/configload` copied from docviewer with its tests, `configs/config.yaml` with page 1 starting values, `.env.example`, `.gitignore`, `deploy/docker-compose.yml` (Postgres 17 with a health check), `.github/workflows/ci.yml`, README stub, this log | `make lint` 0 issues; `make test` green; `make cover` 87.7%; a deliberate `application -> telemetry` import failed `make lint` with the depguard message, then was removed; `make up` brought Postgres to healthy in 4 s and `make down` removed it; diff of `.golangci.yml` against docviewer shows only the depguard block; `pkg/configload` differs only by the test file import path | none (user away) | none; CI "green on first push" is unverified until the repository exists on GitHub (the user creates it, as for docviewer) | see step 00 commit |
| 2026-10-21 | 01 | Foundations: config, errs, observability, telemetry, model | Tests first (5 files, 75 cases: defaults load, 14 validation rows naming the key, 5 env overrides incl. `FINGA_WORLD_TICK_RATE_HZ`, an invalid override; code kept through Wrap and fmt wrapping, errors.As/Is, player message never carries the cause; four classes round trip and unknown rejected without echo, four ID types mint v7, parse, print, JSON; level filtering and `/metrics`; every page 3 metric registered, double registration fails, tick overrun counting); then `internal/config` with `Validate`, `pkg/errs`, `pkg/observability`, `internal/telemetry.Metrics`, `internal/model` IDs and Class | `make test` green on the first run; `make lint` clean after three staticcheck nits in a test (type inference); `make cover` 83.8% | none (user away) | the three lint nits were in test code only; no production change | see step 01 commit |

## Decisions taken while the user was away

Surface decisions the implementation session took on its own, each reversible
and flagged for review. Design questions go to the "finga" session instead.

1. **Placeholder folders.** Page 1 lists the whole tree; step 00 creates the top-level folders and `cmd/gameserver`, `cmd/migrate` with `.gitkeep` files. The `internal/` subtree arrives with the steps that fill it, so no empty package directories sit in the repository.
2. **`make test` runs Go tests only** until `web/` exists (page 1 says it also runs the web tests); the client steps add `npm test`.
3. **CI** installs golangci-lint through its official action and then runs the Makefile targets, so CI and a laptop run the same commands. A Postgres service joins the workflow in step 02, when the first repository tests need it.
4. **`pkg/configload` is byte-identical to docviewer's** apart from the module path in the test file's import. The comment naming `DOCVIEWER_SERVER_ADDR` as an example was left as is, to keep the copy diffable.
5. **Docker Desktop was started by the agent** to run `make up`; it was left running.
6. **`model.Class` lists all four classes** (the column comment on page 2 names them); which classes a player can pick is game data's call, so Stage 1's Warrior and Bowman restriction lives in `data/classes.yaml` (step 04), not in the type.
7. **Two cross-field rules in `config.Validate`:** `database.url` must have a `postgres://` or `postgresql://` scheme, and `world.tick_rate_hz` must divide 60 (at most 60), so each tick holds a whole number of the simulation's 1/60 s sub-steps (page 5). Page 1 only lists the sections; these rules are the implementation's reading of pages 3 and 5.
8. **`pkg/errs` shape:** `New(code, message)`, `Invalid(message)`, `(*Error).Wrap(cause)` returning a copy (so sentinels stay immutable and `errors.Is(copy, sentinel)` holds through code and message equality), `CodeOf` and `MessageOf` taking the outermost `errs` value in the chain, and `InternalMessage` for errors without one. `Error()` includes the cause for logs; `Message` never does.
9. **Metric names are exactly page 3's** (`tick_duration_seconds`, no `finga_` prefix), registered on a registry built by `pkg/observability.NewRegistry` with the Go and process collectors. `Metrics.ObserveTick` records the duration and counts an overrun when a tick exceeds its interval.
