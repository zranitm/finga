# Low-level design

How each part of the system is built, at the level needed to start coding. These pages are durable: they describe the design as first built in Stage 1 and kept from then on. Choices that hold only for Stage 1 are on the Stage 1 page; parts that arrive later are marked **Stage 2**.

Read in this order:

1. **Codebase and modules:** layout, dependency rules, modules, and the generic libraries in `/pkg`.
2. **Data model:** Postgres schemas, constraints, and what stays in memory.
3. **Game server runtime:** goroutines, the map tick, economic actions from a tick, checkpoints.
4. **Protocol:** REST endpoints, WebSocket messages, errors, and limits.
5. **Gameplay rules in code:** maps, movement, stats, combat, mobs, EXP, drops.
6. **Economy and integrity:** ledger, market transactions, idempotency, events, auditor.
7. **Client:** structure, prediction, interpolation, and UI.
8. **Backend implementation steps:** the Stage 1 backend in reviewable steps, in build order.
9. **Client implementation steps:** the browser client, step by step.
10. **Gateway and services:** the split into six services behind a connection gateway (TD-25), and the steps that build it.

Numbers in these pages (speeds, formulas, limits) are starting values. They live in game data so they can be tuned without code changes.

- [1. Codebase and modules](./1-codebase-and-modules.md)
- [2. Data model](./2-data-model.md)
- [3. Game server runtime](./3-game-server-runtime.md)
- [4. Protocol](./4-protocol.md)
- [5. Gameplay rules in code](./5-gameplay-rules-in-code.md)
- [6. Economy and integrity](./6-economy-and-integrity.md)
- [7. Client](./7-client.md)
- [8. Backend implementation steps](./8-backend-implementation-steps.md)
- [9. Client implementation steps](./9-client-implementation-steps.md)
- [10. Gateway and services](./10-gateway-and-services.md)
