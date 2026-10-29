# Backend steps

| Step | Depends on | Features | Milestone | Order | Status |
| --- | --- | --- | --- | --- | --- |
| [00. Repository skeleton](./00-repository-skeleton.md) | None |  | Foundations | 0 | Done |
| [01. Foundations: config, errors, logging, shared types](./01-foundations-config-errors-logging-shared-types.md) | 00 |  | Foundations | 1 | Done |
| [02. Database foundation](./02-database-foundation.md) | 01 |  | Foundations | 2 | Done |
| [03. Movement simulation](./03-movement-simulation.md) | 00 | B2 | Foundations | 3 | Done |
| [04. Game data](./04-game-data.md) | 01, 03 | B1 | Foundations | 4 | Done |
| [05. Accounts and sessions](./05-accounts-and-sessions.md) | 02 | A1 | Foundations | 5 | Done |
| [06. Characters and stats](./06-characters-and-stats.md) | 02, 04 | A2, C4 | Foundations | 6 | Done |
| [07. REST API and server wiring](./07-rest-api-and-server-wiring.md) | 05, 06 | A1, A2, B6 | M1. Walk together | 7 | Done |
| [08. WebSocket transport and message catalogue](./08-websocket-transport-and-message-catalogue.md) | 01 |  | M1. Walk together | 8 | Done |
| [09. World: map instances and the tick](./09-world-map-instances-and-the-tick.md) | 03, 04, 06 | B1, B2 | M1. Walk together | 9 | Done |
| [10. Gateway](./10-gateway.md) | 05, 08, 09 | B2 | M1. Walk together | 10 | Done |
| [11. Mobs, combat, and EXP](./11-mobs-combat-and-exp.md) | 06, 09, 10 | C1, C2, C3, C4 | M2. Fight and grow | 11 | Done |
| [12. Items and inventory](./12-items-and-inventory.md) | 02, 04, 06 | A2, D2, D5 | M3. Loot and keep | 12 | Done |
| [13. Drops and pickup](./13-drops-and-pickup.md) | 11, 12 | D1, D2, D5 | M3. Loot and keep | 13 | Done |
| [14. Portals and towns](./14-portals-and-towns.md) | 09, 10, 11 | B3, B5 | M4. Travel | 14 | Done |
| [15. Chat and party](./15-chat-and-party.md) | 10, 11, 13, 14 | E1, E3 | M5. Play together | 15 | Done |
| [16. Session hardening](./16-session-hardening.md) | 05, 10 | A1 | M6. Proof session | 16 | Done |
| [17. Packaging and proof session](./17-packaging-and-proof-session.md) | All earlier steps |  | M6. Proof session | 17 | Done |
| [18. Client follow-up: one Input per tick](./18-client-follow-up-one-input-per-tick.md) | 09, 10 | B2 | M1. Walk together | 18 | Done |
| [19. Client follow-up: EXP after death and invulnerability](./19-client-follow-up-exp-after-death-and-invulnerability.md) | 11 | C1, C2, C3, C4 | M2. Fight and grow | 19 | Done |
| [20. Shared kernel](./20-shared-kernel.md) | 19 |  | Services | 20 | Done |
| [21. Migrations per service](./21-migrations-per-service.md) | 20 |  | Services | 21 | Done |
| [22. gRPC foundation](./22-grpc-foundation.md) | 20, 21 |  | Services | 22 | Done |
| [23. Item service](./23-item-service.md) | 22 | D2, D5 | Services | 23 | Done |
| [24. Account service](./24-account-service.md) | 22 | A1 | Services | 24 | Done |
| [25. Social service](./25-social-service.md) | 22 | E1, E3 | Services | 25 | Done |
| [26. Character service](./26-character-service.md) | 23 | A2, C4 | Services | 26 | Done |
| [27. World service](./27-world-service.md) | 23, 25, 26 | B1, B2, B3, C1 to C4, D1 | Services | 27 | Done |
| [28. Gateway service](./28-gateway-service.md) | 24, 27 | A1, B2 | Services | 28 | Done |
| [29. Configuration per service](./29-configuration-per-service.md) | 28 |  | Layers | 29 | Done |
| [30. Shared kernel rename and layer rules](./30-shared-kernel-rename-and-layer-rules.md) | 29 |  | Layers | 30 | Ready |
| [31. Account in layers](./31-account-in-layers.md) | 30 |  | Layers | 31 | Ready |
| [32. Item in layers](./32-item-in-layers.md) | 31 |  | Layers | 32 | Ready |
| [33. Character in layers](./33-character-in-layers.md) | 32 |  | Layers | 33 | Ready |
| [34. Social in layers](./34-social-in-layers.md) | 33 |  | Layers | 34 | Ready |
| [35. World in layers](./35-world-in-layers.md) | 34 |  | Layers | 35 | Ready |
| [36. Gateway in layers](./36-gateway-in-layers.md) | 35 |  | Layers | 36 | Ready |
