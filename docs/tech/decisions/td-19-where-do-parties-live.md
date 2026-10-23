# TD-19. Where do parties live?

- **Area:** Architecture
- **Decision:** TD-19. Where do parties live?
- **Default:** In memory, lost on restart
- **Status:** Open

## Options

- **A.** In memory in one actor; parties are lost when the server restarts.
- **B.** Persisted in Postgres.

## Why the default

Parties are short-lived and rebuilding one after a restart takes seconds. Revisit at the gateway split in Stage 2, when parties must span nodes.

## Affects

Low-level design: Game server runtime, Gameplay rules in code.
