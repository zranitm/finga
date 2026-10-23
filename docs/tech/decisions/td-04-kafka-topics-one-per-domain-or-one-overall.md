# TD-04. Kafka topics: one per domain, or one overall?

- **Area:** Architecture
- **Decision:** TD-04. Kafka topics: one per domain, or one overall?
- **Default:** One per domain, keyed by aggregate ID
- **Status:** Open

## Options

- **A.** One topic per domain: `economy.events`, `item.events`, `character.events`.
- **B.** One topic for all events.

## Why the default

Consumers subscribe only to what they need, and keying by aggregate ID keeps events for one wallet or item in order.

## Affects

HLD section 7; the outbox relay and consumers.
