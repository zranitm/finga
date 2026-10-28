# 17. Packaging and proof session

- **Depends on:** All earlier steps
- **Milestone:** M6. Proof session
- **Order:** 17
- **Status:** Done
- **Step:** 17. Packaging and proof session

> **Changed by step 23 (2026-10-27):** Compose runs the item service beside the game server; the Dockerfile's build argument is `SERVICE`.

> **Changed by step 27 (2026-10-28):** Compose runs the world service and publishes its ops port (9104); `make bots` reads the tick percentiles there (`-metrics`).

> **Changed by step 28 (2026-10-28):** Compose runs Postgres, migrate and the six services, the gateway on :8080; `make down` also removes containers of services the Compose file no longer has; CI tests each service on its own and runs `buf breaking`.

**Goal:** `make up` runs the whole Stage 1 game from a clean clone, ready for the proof session.

## Scope

- `deploy/Dockerfile`: one file for every binary, picked by a build argument, on a distroless base as in docviewer.
- `deploy/docker-compose.yml`: Postgres with a health check, `migrate` as a one-shot job, `gameserver` once the migration succeeds, and `web` (nginx serving the built client, once the frontend exists).
- README: build and run, configuration, tests, architecture at a glance, and how this was built with AI, drawn from `docs/ai-coding-log.md`.
- The crash test from step 13 runs in CI.
- A proof-session checklist built from Stage 1's exit criteria.

## Tests first

- a CI job builds the images and runs `make up`, then a test client signs up, enters the world, and moves
- one end-to-end row where a client re-sends the same PickUp (same sequence number; PickUp carries no request ID) after ignoring the first reply, and still ends with one item and one event: the path a real client takes after a lost reply, kept in the evidence log next to the crash test (asked at the M3 review, 2026-10-22)

## Evidence the final report must carry (asked at the M5 review, 2026-10-22)

- The 30 to 60 minute session with 3 to 4 players (bots are fine when no humans are available; say which).
- Tick p99 under 25 ms, with the number measured.
- The crash test with the invariants intact.
- The double and retried pickup rows, including the WebSocket re-send with the same seq.
- All of it in [docs/ai-coding-log.md](../../../ai-coding-log.md).

## Done when

- [ ] `make up` works from a clean clone.
- [ ] The exit-criteria checklist is ready for the playtest.

## Review focus

- Nothing in the images or configs that should be secret.
