# TD-14. Kafka for durable domain events

- **Area:** Architecture
- **Decision:** TD-14. Kafka for durable domain events
- **Default:** Kafka for durable domain events
- **Outcome:** Kafka is part of the target architecture, for durable domain events through the outbox. It arrives in Stage 2 to keep Stage 1's setup minimal; Stage 1 already writes every event to the outbox table, so adding Kafka changes no business logic.
- **Status:** Decided

## Scope

- **On Kafka:** domain events from the outbox, such as trades, loot, and level-ups.
- **Off Kafka:** movement, combat ticks, and anything that must respond in milliseconds.
