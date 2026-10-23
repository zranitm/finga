# TD-16. What sits between the client and the game server?

- **Area:** Architecture
- **Decision:** TD-16. What sits between the client and the game server?
- **Default:** A game gateway, no BFF
- **Outcome:** A game gateway, not a BFF. There's one client and the traffic is a WebSocket stream, so a BFF would have nothing to aggregate.
- **Status:** Decided

## Gateway responsibilities

WebSocket sessions, auth handshake, one active session per account, rate limits, routing to map instances, fan-out, and seamless handoff between nodes later.

## Phases

A package inside the game server in the MVP; its own service once channel servers split.
