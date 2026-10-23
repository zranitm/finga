# 00. Repository skeleton

- **Depends on:** None
- **Milestone:** Foundations
- **Order:** 0
- **Status:** Done
- **Step:** 00. Repository skeleton

**Goal:** an empty repository with the agreed layout and tooling, green from the first commit.

## Scope

- Folders as in Low-level design page 1. `go.mod` with `module finga` and Go 1.27.
- Copied from docviewer: `.golangci.yml`, `.gitattributes`, the Makefile's `up`, `down`, `test`, `cover`, and `lint` targets, and `pkg/configload` with its tests.
- `.golangci.yml` gains `depguard` rules for the import direction on page 1: a handler importing a repository's generated code, or the application importing `protocol`, `telemetry`, or `config`, fails `make lint`.
- A `gen` Make target (sqlc), empty for now.
- `.gitignore`: `.env`, `configs/config.local.yaml`, `bin/`, coverage files, `web/node_modules/`, `web/dist/`, `web/public/maps/`.
- `configs/config.yaml` with the starting values from page 1, and `.env.example` listing every `FINGA_` override, commented out.
- `deploy/docker-compose.yml` with Postgres only, with a health check.
- `.github/workflows/ci.yml`: `make lint`, `make test`, `make cover`.
- `docs/ai-coding-log.md` with its header row, and a README stub: what Finga is, prerequisites, Make targets.

## Tests first

`pkg/configload` brings its own tests. Nothing else yet.

## Done when

- [ ] `make lint` and `make test` pass.
- [ ] `make up` starts Postgres and `make down` stops it.
- [ ] CI is green on the first push.
- [ ] The tree matches page 1.
- [ ] A deliberately wrong import, tried once, fails `make lint`.

## Review focus

- `pkg/configload` is an unchanged copy, and `.golangci.yml` differs from docviewer's only by the `depguard` rules (diff both).
- Commits use your personal e-mail, set in the repository's own git config.
