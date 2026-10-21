# AI coding log

One row per backend implementation step (Low-level design, page 8). Evidence
for the process: what was produced, how it was verified, what changed,
mistakes caught, and the commit. Moved to Notion by the design session.

Tool for every row: Claude Code, model Claude Fable 5.1 (`claude-fable-5-1`),
running as the "finga-impl" session from the Notion product and tech pages.

| Date | Step | Task | What it produced | How verified | What the user changed | Mistakes caught | Commit |
|------|------|------|------------------|--------------|-----------------------|-----------------|--------|
| 2026-10-21 | 00 | Repository skeleton | Layout per LLD page 1 (top-level folders, `cmd/gameserver`, `cmd/migrate`), `go.mod` (module finga, go 1.27), Makefile (up/down/test/cover/lint/gen), `.golangci.yml` with depguard rules for the import direction, `pkg/configload` copied from docviewer with its tests, `configs/config.yaml` with page 1 starting values, `.env.example`, `.gitignore`, `deploy/docker-compose.yml` (Postgres 17 with a health check), `.github/workflows/ci.yml`, README stub, this log | `make lint` 0 issues; `make test` green; `make cover` 87.7%; a deliberate `application -> telemetry` import failed `make lint` with the depguard message, then was removed; `make up` brought Postgres to healthy in 4 s and `make down` removed it; diff of `.golangci.yml` against docviewer shows only the depguard block; `pkg/configload` differs only by the test file import path | none (user away) | none; CI "green on first push" is unverified until the repository exists on GitHub (the user creates it, as for docviewer) | see step 00 commit |

## Decisions taken while the user was away

Surface decisions the implementation session took on its own, each reversible
and flagged for review. Design questions go to the "finga" session instead.

1. **Placeholder folders.** Page 1 lists the whole tree; step 00 creates the top-level folders and `cmd/gameserver`, `cmd/migrate` with `.gitkeep` files. The `internal/` subtree arrives with the steps that fill it, so no empty package directories sit in the repository.
2. **`make test` runs Go tests only** until `web/` exists (page 1 says it also runs the web tests); the client steps add `npm test`.
3. **CI** installs golangci-lint through its official action and then runs the Makefile targets, so CI and a laptop run the same commands. A Postgres service joins the workflow in step 02, when the first repository tests need it.
4. **`pkg/configload` is byte-identical to docviewer's** apart from the module path in the test file's import. The comment naming `DOCVIEWER_SERVER_ADDR` as an example was left as is, to keep the copy diffable.
5. **Docker Desktop was started by the agent** to run `make up`; it was left running.
