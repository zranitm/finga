# 8. Backend implementation steps

The Stage 1 backend, split into steps you review one at a time before any code is written. Each step page lists its scope, design notes, the tests to write first, what "done" means, and what to look at closely. The design itself stays in pages 1 to 6; a step page only says what to build in that step.

## How a step runs

1. **Review:** you read the step page. Status moves from Ready for review to Approved, or to Changes requested with comments.
2. **Tests first:** the tests listed on the page are written before the code. The test tables are the main review surface: each case name says what it proves.
3. **Build:** only what the step's scope lists, on top of steps already done.
4. **Green:** `make test`, `make lint`, and `make cover` pass, then one commit for the step.
5. **Log:** one row in `docs/ai-coding-log.md`: what was produced, how it was verified, what changed, mistakes caught, and the commit. Status moves to Done.

## Rules

- **Bottom-up:** steps follow the dependency graph, so each one builds only on tested code.
- **Small:** a step is one reviewable unit. If one grows past a day or two of work, it's split.
- **The design wins:** if a step shows that a design page is wrong, that page is fixed in the same step, not only the step page.
- **Stage 1 only:** nothing from Stage 2 (currency, market, Kafka, the auditor, Protobuf on the client's WebSocket). The internal gRPC contracts of steps 20 to 28 are Protobuf, at the user's decision to split in Stage 1 (TD-25).
- **Open decisions:** a step that depends on one builds the current default and names it.
- **By layer:** each step says which layers it builds (page 1, Layers and responsibilities), and groups its scope and tests by layer. Repository tests run against real Postgres, service rule tests use fakes, and handler tests use fake services.

## Milestones

| Milestone (Stage 1 page) | Steps |
| --- | --- |
| Foundations | 00 to 06 |
| M1. Walk together | 07 to 10 |
| M2. Fight and grow | 11 |
| M3. Loot and keep | 12, 13 |
| M4. Travel | 14 |
| M5. Play together | 15 |
| M6. Proof session | 16, 17 |
| Services (page 10, TD-25) | 20 to 28 |
| Layers (page 11) | 29 to 36 |

The frontend has its own steps against page 7: [9. Client implementation steps](./9-client-implementation-steps.md).

- [Backend steps](./backend-steps/README.md)
