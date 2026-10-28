# 26. Character service

- **Depends on:** 23
- **Features:** A2, C4
- **Milestone:** Services
- **Order:** 26
- **Status:** Done
- **Step:** 26. Character service

**Goal:** characters run as their own service: REST lists and creates them, Hello loads them, and the world's checkpoints and level-ups save them, all over gRPC.

**Layers in this step:** the character module (moved), its handler, its item client and binary; the game server's adapters.

## Scope

- `internal/application/character` moves to `services/character/internal/character` as it is.
- `services/character/internal/handler`: `CharacterService` with its error table; `Load` returns the character with its maximum HP and MP.
- `services/character/internal/clients`: the item client for `GrantStarter` (moved from the game server).
- `services/character/cmd/character` and `app`.
- Game server: REST's characters, Hello's load, and the world's `Saver` call the character service. The persistence worker retries a failed save with backoff, in order, for up to a minute, then logs it and moves on.
- `errmap` maps the character reasons to the same codes. Compose and the end-to-end stack run the character service.

## Tests first

- handler over `bufconn`: each RPC maps its fields (table); each character error arrives with its reason (table); the guard; `Load` of someone else's character is `character_not_found`
- persistence worker: a save that fails twice then succeeds is retried and the next job waits for it; one that keeps failing is dropped after the limit and the worker goes on
- end-to-end rows unchanged: A2, character slots, the C4 level-up surviving a reconnect

## Done when

- [ ] `make up`, `make smoke`, `make test`, `make lint`, `make cover` green.

## Review focus

- The worker's retry keeps the jobs in order, so an older checkpoint never lands after a newer one.
