# Finga

A browser-based 2D side-scrolling MMORPG-lite: pick a class, hunt mobs, level
up, team up in parties of up to 4, and loot. Go game server, Phaser client,
Postgres. The design lives in Notion and is exported to `docs/`.

This repository is at Stage 1 (proof of concept): the backend is built in the
steps of Low-level design page 8, one commit per step, with the evidence trail
in `docs/ai-coding-log.md`.

## Prerequisites

- Docker with Compose
- Go 1.27 or newer (a C compiler is needed for the race detector; on Windows,
  for example `winget install BrechtSanders.WinLibs.POSIX.UCRT`)
- make
- golangci-lint v2, for `make lint`
- sqlc, for `make gen`

## Make targets

```bash
make up      # start Postgres (later: migrations, the game server on :8080, the client on :5173)
make down    # stop everything and drop the database volume
make test    # every test, with the race detector
make cover   # coverage report; fails below 80% of statements in internal/ and pkg/
make lint    # golangci-lint, including the import-direction rules (depguard)
make gen     # regenerate sqlc code
```

## Configuration

Defaults live in `configs/config.yaml`. Any key can be overridden as `FINGA_`
plus its path in upper case, for example `FINGA_SERVER_ADDR=:9090`. Copy
`.env.example` to `.env` (git-ignored) for local overrides; precedence, lowest
to highest: the YAML file, `.env`, real environment variables.
