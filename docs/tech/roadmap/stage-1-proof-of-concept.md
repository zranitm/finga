# Stage 1: Proof of concept

> ⏳ **Temporary page.** Everything here is true only during Stage 1. Permanent decisions live in Product decisions and Tech decisions. Once Stage 1 is done and anything permanent has been promoted, this page can be deleted.

## Goal

Prove the idea works: a few friends play together in the browser for a full session, and the economy stays correct. Keep the setup minimal while still running the real business logic.

## What it must prove

- The core loop is fun with friends: hunt, level up, party up, loot.
- Real-time movement and combat feel smooth in a browser.
- Item integrity holds: no item is ever duplicated or lost, with party loot in play.

## Exit criteria

- 3–4 players finish a 30–60 minute session together: create characters, hunt, level up, form a party, and loot.
- Movement and combat feel smooth on a normal home connection, and tick processing p99 stays under 25 ms.
- The crash test passes: the server is killed mid-run, restarted, and every item invariant still holds.
- Two players picking up the same drop, and a retried pickup, each result in exactly one item.

## Product scope

Feature details are in [Features](../../product/features/README.md).

- **Account and character:** A1, A2
- **World and movement:** B1, B2, B3, B5, B6
- **Combat and progression:** C1, C2, C3, C4
- **Items and inventory:** D1, D2, D5
- **Social:** E1, E3
- **Economy:** none. Currency (F1) and the marketplace (F3) move to Stage 2, so mobs drop items only.
- **Content:** 1 town, 2 hunting maps, 3–4 mob types, about 10 items. Classes: Warrior and Bowman (S1-Q1).

## Tech scope

Three moving parts:

- **Client:** Phaser with TypeScript in `web/`. The Vite dev server while developing, and `vite preview` of the production build for a play session (`make play`); a container serving the built files joins when the game is deployed.
- **Game services:** six Go binaries in `server/services/` (gateway, account, character, item, world, social), one node each, calling each other over gRPC (TD-25, Low-level design page 10). The gateway is the only public entry, with REST and the WebSocket, and holds no game rules and no database. `cmd/migrate` applies every service's migrations.
- **Postgres:** with pgx, sqlc, and goose; one database, each service with its own tables and migrations.
- **Tooling:** Docker Compose with Postgres, the migrate job and the six services. GitHub Actions runs `make test`, `make cover`, `make lint`, the client build, each service built and tested on its own, `buf breaking` on the gRPC contracts, and the smoke test against `make up`.

## Not simplified, even in Stage 1

These are expensive to retrofit, so they're built to the target from day one:

- Server authority over movement, combat, and mobs.
- Item events, idempotency keys, and single-transaction changes to items. The ledger arrives in Stage 2 with currency, built the same way.
- Service boundaries: each module lives in a service and owns its tables; services reach each other only through their gRPC contracts.
- Generic libraries in `/pkg` that never import game code.
- Own account module behind `pkg/auth` (TD-17).

## Temporary choices

| Area | Stage 1 | Target | Seam that keeps the switch cheap |
| --- | --- | --- | --- |
| Event publishing | Events written to the outbox table; nothing publishes them yet | Outbox relay to Kafka (TD-14) | The outbox table and the `pkg/outbox` writer already exist |
| Auditor | None. The item invariants are checked inside the property, race, retry, and crash tests | A Kafka consumer that checks continuously (HLD section 4) | The checks are plain functions in `services/item/invariants`, which an auditor process can run as they are |
| Message encoding | JSON over WebSocket | Protobuf (HLD section 8) | A codec interface in `pkg/wsconn` |
| Edge | None. Vite serves the client (:5173, or :4173 for `make play`), and the browser calls the gateway on :8080 directly, allowed by `server.cors_origins` (as in docviewer) | nginx and Cloudflare in front of both, on one origin (TD-12, TD-13) | The client reads the API origin from build config, and paths are already `/api` and `/ws` |
| Observability | A `/metrics` endpoint and structured logs | Prometheus and Grafana, or OpenTelemetry (TD-21) | Metric names stay the same |
| Load testing | None | Bot load tests to the TD-08 target | None needed |
| Hosting | Local only (S1-Q2) | One VPS (TD-05) | The same Docker Compose setup |
| Stat allocation | Automatic: each level-up adds 5 points by class preset (Warrior: STR 3, VIT 2; Bowman: DEX 3, AGI 2) | Players spend their own points (OD-16) | Level-up takes an allocation plan; Stage 1 always passes the class preset |

## Stage decisions

- **S1-Q1. Classes in Stage 1:** decided: two, Warrior and Bowman (one melee, one ranged). The final game has four (OD-03); Thief and Magician arrive in Stage 2.
- **S1-Q2. Public link or local only:** decided: local only. nginx and Cloudflare arrive in Stage 2.
- **S1-Q3. One binary or services:** decided by the user on 2026-10-27: services, one node each, behind a connection gateway (TD-25). Built in backend steps 20 to 28 (Low-level design, page 10); the tech scope above describes it as built.

## Milestones

Each one is playable and ends with something to show.

1. **Walk together:** A1, A2, B1, B2, B6. Sign up, create a character, and see friends moving on one map.
2. **Fight and grow:** C1, C2, C3, C4. Mobs, attacks, death, EXP, and levels.
3. **Loot and keep:** D1, D2, D5. Item drops and inventory, with the double-pickup, retry, and crash tests.
4. **Travel:** B3, B5. Town and hunting maps connected by portals.
5. **Play together:** E1, E3. Map chat and parties with shared EXP.
6. **Proof session:** run the exit criteria with real players.

## Local setup

- `make up` starts Postgres, the migrations and the six services, the gateway on :8080; `npm --prefix web run dev` serves the client on :5173 (Vite proxies `/api` and `/ws`). `make down` stops everything.
- Outside Docker: run Postgres yourself, then `make run` applies every service's migrations and starts the six binaries.
- `make test`, `make cover`, and `make lint` before each commit.
- For a playtest with friends on the same network: `make up`, then add `http://<your LAN IP>:5173` to `FINGA_SERVER_CORS_ORIGINS` in `.env`.
